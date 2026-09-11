// dualstack_reconcile.go 双栈租约对齐（M3-013，§4.5 匹配方案链）。
//
// 方案链（auto，命中即止）：admin（人工映射，权威）→ option79（租约 hw-address）
// → client-id（RFC4361：v4 client-id 为 DUID 形态）→ duid-llt（DUID 内嵌 MAC）
// → hostname（唯一才生效）→ 冲突清单（同名歧义/无 MAC 信号）。
// 模板 matchScheme 钉死时，命中方式不符记 pinned_mismatch，不做跨方案回落。
// 精确方式命中自动落 dualstack_identity（source=auto）；hostname 启发式不落表。
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

// adminIdentity dualstack_identity 行投影。
type adminIdentity struct {
	Mac  string
	Duid string
}

// identityIndex 方案链解析所需的身份索引（每周期构建一次）。
type identityIndex struct {
	adminByDuid  map[string]string   // duid → mac（dualstack_identity，权威覆盖）
	macByDuid    map[string]string   // duid → mac（v4 client-id RFC4361 桥）
	v4ByHostname map[string][]Lease4 // hostname → v4 租约（唯一性判定）
	v4ByMac      map[string]Lease4   // mac（去冒号小写）→ v4 租约
}

func macKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(NormalizeMAC(s), ":", ""))
}

func normalizeHost(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// isDUIDForm 判定 v4 client-id 是否 RFC4361 的 DUID 形态（00:01~00:04）；01:MAC 传统形态返回 false。
func isDUIDForm(cid string) bool {
	c := strings.ToLower(strings.ReplaceAll(cid, ":", ""))
	return strings.HasPrefix(c, "0001") || strings.HasPrefix(c, "0002") ||
		strings.HasPrefix(c, "0003") || strings.HasPrefix(c, "0004")
}

// macFromDUID DUID-LLT（type 0001）末 6 字节提取 MAC（去冒号小写）；其余类型返回空。
func macFromDUID(duid string) string {
	d := strings.ToLower(strings.ReplaceAll(duid, ":", ""))
	if !strings.HasPrefix(d, "0001") || len(d) < 28 {
		return ""
	}
	return d[len(d)-12:]
}

func buildIdentityIndex(l4 []Lease4, admin []adminIdentity) *identityIndex {
	idx := &identityIndex{
		adminByDuid:  map[string]string{},
		macByDuid:    map[string]string{},
		v4ByHostname: map[string][]Lease4{},
		v4ByMac:      map[string]Lease4{},
	}
	for _, a := range admin {
		if a.Mac != "" && a.Duid != "" {
			idx.adminByDuid[a.Duid] = a.Mac
		}
	}
	for _, l := range l4 {
		if l.State != 0 {
			continue
		}
		mac := macKey(l.HWAddress)
		if mac == "" {
			continue
		}
		idx.v4ByMac[mac] = l
		if isDUIDForm(l.ClientID) {
			idx.macByDuid[normalizeDUID(l.ClientID)] = mac
		}
		if h := normalizeHost(l.Hostname); h != "" {
			idx.v4ByHostname[h] = append(idx.v4ByHostname[h], l)
		}
	}
	return idx
}

// resolve 按方案链解析 v6 租约对应的 v4 MAC；reason 非空表示未解析（进冲突清单）。
func (idx *identityIndex) resolve(v6 Lease6) (mac, method, reason string) {
	duid := normalizeDUID(v6.DUID)
	if m, hit := idx.adminByDuid[duid]; hit {
		return m, "admin", ""
	}
	if v6.HWAddress != "" {
		if m := macKey(v6.HWAddress); m != "" {
			return m, "option79", ""
		}
	}
	if m, hit := idx.macByDuid[duid]; hit {
		return m, "client-id", ""
	}
	if m := macFromDUID(v6.DUID); m != "" {
		return m, "duid-llt", ""
	}
	h := normalizeHost(v6.Hostname)
	if h != "" {
		if l4s := idx.v4ByHostname[h]; len(l4s) == 1 {
			if m := macKey(l4s[0].HWAddress); m != "" {
				return m, "hostname", ""
			}
		} else if len(l4s) > 1 {
			return "", "", "ambiguous_hostname"
		}
	}
	return "", "", "no_mac_signal"
}

// schemeAllows 钉死判定：auto/空接受任意命中方式；指定值只接受同方式（跨方案回落进冲突）。
func schemeAllows(pinned, method string) bool {
	return pinned == "" || pinned == "auto" || pinned == method
}

type conflictRow struct {
	duid     string
	v6ip     string
	macs     []string
	hostname string
	reason   string
}

// ReconcileDualstackLeases 按方案链对齐 kea6 租约到模板规范地址，并记录冲突/自动学习映射。
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
	if len(tpls) == 0 {
		return nil
	}
	subnets, err := fetchKea6Subnets(ctx, agentURL)
	if err != nil {
		return err
	}
	admin, err := loadAdminIdentities(ctx, pool)
	if err != nil {
		return err
	}
	idx := buildIdentityIndex(l4, admin)
	conflicts := map[string]conflictRow{}

	for _, v6 := range l6 {
		if v6.Type != "IA_NA" || v6.State != 0 {
			continue
		}
		duid := normalizeDUID(v6.DUID)
		mac, method, reason := idx.resolve(v6)
		if reason != "" {
			cr := conflictRow{duid: duid, v6ip: v6.IPAddress, hostname: normalizeHost(v6.Hostname), reason: reason}
			if reason == "ambiguous_hostname" {
				for _, l := range idx.v4ByHostname[normalizeHost(v6.Hostname)] {
					if m := macKey(l.HWAddress); m != "" {
						cr.macs = append(cr.macs, m)
					}
				}
			}
			conflicts[duid] = cr
			continue
		}
		l4lease, hit := idx.v4ByMac[mac]
		if !hit {
			conflicts[duid] = conflictRow{duid: duid, v6ip: v6.IPAddress, macs: []string{mac},
				hostname: normalizeHost(v6.Hostname), reason: "no_mac_signal"}
			continue
		}
		tpl, merr := MatchIPv4Template(tpls, l4lease.IPAddress)
		if merr != nil {
			continue
		}
		if !schemeAllows(tpl.MatchScheme, method) {
			conflicts[duid] = conflictRow{duid: duid, v6ip: v6.IPAddress, macs: []string{mac},
				hostname: normalizeHost(v6.Hostname), reason: "pinned_mismatch"}
			continue
		}
		want, aerr := ApplyTemplate(tpl, l4lease.IPAddress)
		if aerr != nil {
			log.Printf("[dualstack-reconcile] %s: 地址计算失败 %s: %v", normalizeHost(v6.Hostname), l4lease.IPAddress, aerr)
			continue
		}
		if method == "option79" || method == "client-id" || method == "duid-llt" {
			learnIdentity(ctx, pool, mac, duid)
		}
		needRewrite := !strings.EqualFold(want, v6.IPAddress) ||
			(v6.Hostname == "" && strings.TrimSuffix(l4lease.Hostname, ".") != "")
		if !needRewrite {
			continue
		}
		sid, ok := subnetIDFor(subnets, want)
		if !ok {
			log.Printf("[dualstack-reconcile] %s: 规范地址 %s 不在任何 kea6 子网内，跳过", normalizeHost(v6.Hostname), want)
			continue
		}
		if err := keaCmd(ctx, agentURL, "lease6-del", "dhcp6", map[string]any{"ip-address": v6.IPAddress}); err != nil {
			log.Printf("[dualstack-reconcile] lease6-del %s: %v", v6.IPAddress, err)
			continue
		}
		args := map[string]any{
			"ip-address": want, "duid": v6.DUID, "iaid": v6.IAID, "subnet-id": sid,
			"valid-lft": v6.ValidLifetime, "cltt": time.Now().Unix(),
			"type": "IA_NA", "prefix-len": 128, "hostname": strings.TrimSuffix(l4lease.Hostname, "."),
		}
		if err := keaCmd(ctx, agentURL, "lease6-add", "dhcp6", args); err != nil {
			log.Printf("[dualstack-reconcile] lease6-add %s: %v", want, err)
			continue
		}
		log.Printf("[dualstack-reconcile] %s: %s -> %s (方式=%s v4 %s)", normalizeHost(v6.Hostname), v6.IPAddress, want, method, l4lease.IPAddress)
	}

	return syncConflicts(ctx, pool, conflicts)
}

// loadAdminIdentities 读取人工/自动映射（权威层）。
func loadAdminIdentities(ctx context.Context, pool *pgxpool.Pool) ([]adminIdentity, error) {
	rows, err := pool.Query(ctx, `SELECT mac, duid FROM dualstack_identity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adminIdentity{}
	for rows.Next() {
		var a adminIdentity
		if err := rows.Scan(&a.Mac, &a.Duid); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// learnIdentity 精确方式命中自动落表（source=auto）；已有人工映射不动，失败静默（尽力而为）。
func learnIdentity(ctx context.Context, pool *pgxpool.Pool, mac, duid string) {
	var existing string
	if err := pool.QueryRow(ctx,
		`SELECT source FROM dualstack_identity WHERE mac=$1 OR duid=$2 LIMIT 1`, mac, duid).Scan(&existing); err == nil && existing == "admin" {
		return
	}
	_, _ = pool.Exec(ctx, `DELETE FROM dualstack_identity WHERE (mac=$1 OR duid=$2) AND source='auto'`, mac, duid)
	_, _ = pool.Exec(ctx,
		`INSERT INTO dualstack_identity(mac, duid, source) VALUES($1,$2,'auto') ON CONFLICT DO NOTHING`, mac, duid)
}

// syncConflicts 覆盖写入当前冲突清单（表小，全量替换）。
func syncConflicts(ctx context.Context, pool *pgxpool.Pool, conflicts map[string]conflictRow) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM dualstack_conflict`); err != nil {
		return err
	}
	for _, c := range conflicts {
		if _, err := tx.Exec(ctx,
			`INSERT INTO dualstack_conflict(duid, v6_ip, v4_macs, hostname, reason, updated_at)
			 VALUES($1,$2,$3,$4,$5,now())`,
			c.duid, c.v6ip, c.macs, c.hostname, c.reason); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
