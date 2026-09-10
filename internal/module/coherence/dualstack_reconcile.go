// dualstack_reconcile.go 双栈租约对齐（§4.3 设计闭环）：按主机名关联 v4/v6 租约，
// 以模板编码（B 型 {v4.hextet4} 十进制镜像）计算规范 v6 地址；kea6 现租约不一致时
// lease6-del + lease6-add 强制对齐（保留 duid/iaid，客户端下次续约即得规范地址）。
// 客户端无 FQDN 或无匹配模板时跳过（保留池分配地址）。
package coherence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReconcileDualstackLeases 对齐 kea6 租约到模板规范地址。
func ReconcileDualstackLeases(ctx context.Context, pool *pgxpool.Pool, agentURL string) error {
	l4, err := fetchLease4(ctx, agentURL)
	if err != nil {
		return err
	}
	l6, err := fetchLease6(ctx, agentURL)
	if err != nil {
		return err
	}
	tl := NewTplLoader(pool)
	if _, err := tl.Refresh(ctx); err != nil {
		return err
	}
	tpls := tl.All()
	log.Printf("[dualstack-reconcile] tpls=%d v4=%d v6=%d", len(tpls), len(l4), len(l6))
	if len(tpls) == 0 {
		return nil
	}
	subnets, err := fetchKea6Subnets(ctx, agentURL)
	if err != nil {
		return err
	}

	byHost := map[string]Lease4{}
	for _, l := range l4 {
		if l.State != 0 {
			continue
		}
		h := strings.ToLower(strings.TrimSuffix(l.Hostname, "."))
		if h != "" {
			byHost[h] = l
		}
	}
	for _, v6 := range l6 {
		if v6.Type != "IA_NA" || v6.State != 0 {
			continue
		}
		h := strings.ToLower(strings.TrimSuffix(v6.Hostname, "."))
		if h == "" {
			// 租约丢主机名（对齐重写后 kea 侧未回填）：从绑定表按 DUID 找回
			var bh string
			if qerr := pool.QueryRow(ctx, `SELECT coalesce(hostname,'') FROM coherence_binding WHERE mac = $1`,
				normalizeDUID(v6.DUID)).Scan(&bh); qerr == nil {
				h = strings.ToLower(strings.TrimSuffix(bh, "."))
			}
		}
		if h == "" {
			continue
		}
		l4, ok := byHost[h]
		if !ok {
			log.Printf("[dualstack-reconcile] %s: 无对应 v4 租约，跳过", h)
			continue
		}
		tpl, err := MatchIPv4Template(tpls, l4.IPAddress)
		if err != nil {
			log.Printf("[dualstack-reconcile] %s: 模板匹配失败 %s: %v", h, l4.IPAddress, err)
			continue
		}
		want, err := ApplyTemplate(tpl, l4.IPAddress)
		if err != nil {
			log.Printf("[dualstack-reconcile] %s: 地址计算失败 %s: %v", h, l4.IPAddress, err)
			continue
		}
		if want == "" {
			continue
		}
		// 地址不一致，或地址已对但租约丢主机名（lease6-add 前次未带）：都需要重写
		needRewrite := !strings.EqualFold(want, v6.IPAddress) ||
			(v6.Hostname == "" && strings.TrimSuffix(l4.Hostname, ".") != "")
		if !needRewrite {
			continue
		}
		log.Printf("[dualstack-reconcile] %s: 待对齐 %s -> %s", h, v6.IPAddress, want)
		sid, ok := subnetIDFor(subnets, want)
		if !ok {
			log.Printf("[dualstack-reconcile] %s: 规范地址 %s 不在任何 kea6 子网内，跳过", h, want)
			continue
		}
		if err := keaCmd(ctx, agentURL, "lease6-del", "dhcp6", map[string]any{"ip-address": v6.IPAddress}); err != nil {
			log.Printf("[dualstack-reconcile] lease6-del %s: %v", v6.IPAddress, err)
			continue
		}
		args := map[string]any{
			"ip-address": want, "duid": v6.DUID, "iaid": v6.IAID, "subnet-id": sid,
			"valid-lft": v6.ValidLifetime, "cltt": time.Now().Unix(),
			"type": "IA_NA", "prefix-len": 128, "hostname": strings.TrimSuffix(l4.Hostname, "."),
		}
		if err := keaCmd(ctx, agentURL, "lease6-add", "dhcp6", args); err != nil {
			log.Printf("[dualstack-reconcile] lease6-add %s: %v", want, err)
			continue
		}
		log.Printf("[dualstack-reconcile] %s: %s -> %s (v4 %s)", h, v6.IPAddress, want, l4.IPAddress)
	}
	return nil
}

type kea6Subnet struct {
	ID     uint32
	Prefix netip.Prefix
}

// fetchKea6Subnets 拉取 kea6 运行态子网（subnet-id 定位用）。
func fetchKea6Subnets(ctx context.Context, agentURL string) ([]kea6Subnet, error) {
	body, _ := json.Marshal(map[string]any{"command": "config-get", "service": []string{"dhcp6"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed []struct {
		Result    int `json:"result"`
		Arguments struct {
			Dhcp6 struct {
				Subnet6 []struct {
					ID     uint32 `json:"id"`
					Subnet string `json:"subnet"`
				} `json:"subnet6"`
			} `json:"Dhcp6"`
		} `json:"arguments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed) == 0 || parsed[0].Result != 0 {
		return nil, fmt.Errorf("config-get dhcp6 failed")
	}
	out := []kea6Subnet{}
	for _, s := range parsed[0].Arguments.Dhcp6.Subnet6 {
		if p, perr := netip.ParsePrefix(s.Subnet); perr == nil {
			out = append(out, kea6Subnet{ID: s.ID, Prefix: p})
		}
	}
	return out, nil
}

func subnetIDFor(subnets []kea6Subnet, addr string) (uint32, bool) {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return 0, false
	}
	for _, s := range subnets {
		if s.Prefix.Contains(ip) {
			return s.ID, true
		}
	}
	return 0, false
}
