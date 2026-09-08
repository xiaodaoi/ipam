// lease_sync.go 租约→绑定同步：轮询 Kea lease4-get-all，收敛 coherence_binding
// （台账在线状态/联动记录/ResolveBinding 的数据源；写入方此前缺失导致台账无在线态，M3-012）。
package coherence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Lease4 Kea lease4-get-all 行投影。
type Lease4 struct {
	IPAddress     string `json:"ip-address"`
	HWAddress     string `json:"hw-address"`
	Hostname      string `json:"hostname"`
	Cltt          int64  `json:"cltt"`
	ValidLifetime uint32 `json:"valid-lft"`
	State         int    `json:"state"` // 0=default 1=declined
	SubnetID      uint32 `json:"subnet-id"`
}

type keaLeaseResp struct {
	Result    int    `json:"result"`
	Text      string `json:"text"`
	Arguments struct {
		Leases []Lease4 `json:"leases"`
	} `json:"arguments"`
}

// SyncLease4Bindings 拉取 Kea v4 租约并收敛 coherence_binding：
// 上报租约 upsert 为 active；消失的 active 绑定删除（对偶 ReportLease 生命周期语义）。
// 完成后 NOTIFY coherence_change（daemon 内存 store 增量刷新）。
func SyncLease4Bindings(ctx context.Context, pool *pgxpool.Pool, agentURL string) error {
	leases, err := fetchLease4(ctx, agentURL)
	if err != nil {
		return err
	}
	seen := make([]string, 0, len(leases))
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, l := range leases {
		if l.State != 0 || l.HWAddress == "" || l.IPAddress == "" {
			continue
		}
		mac := NormalizeMAC(l.HWAddress)
		if mac == "" {
			continue
		}
		host := strings.TrimSuffix(l.Hostname, ".")
		if _, err := tx.Exec(ctx,
			`INSERT INTO coherence_binding(mac, ipv4, ipv6, hostname, state, cltt, valid_lft, last_seen)
			 VALUES($1, $2, '::', $3, 'active', $4, $5, now())
			 ON CONFLICT (mac) DO UPDATE SET ipv4=$2, hostname=$3, state='active',
			   cltt=$4, valid_lft=$5, last_seen=now()`,
			mac, l.IPAddress, host, l.Cltt, int(l.ValidLifetime)); err != nil {
			return err
		}
		seen = append(seen, mac)
	}
	if len(seen) > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM coherence_binding WHERE state='active' AND ipv6='::' AND mac != ALL($1::text[])`,
			seen); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `DELETE FROM coherence_binding WHERE state='active' AND ipv6='::'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `NOTIFY coherence_change`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func fetchLease4(ctx context.Context, agentURL string) ([]Lease4, error) {
	// 注意：不带 arguments（空对象会被 Kea 2.2 判为“subnets 未指定”）；
	// 无参 = 查询全部子网租约。
	body, _ := json.Marshal(map[string]any{
		"command": "lease4-get-all",
		"service": []string{"dhcp4"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed []keaLeaseResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("empty lease4-get-all response")
	}
	if r := parsed[0]; r.Result != 0 && r.Result != 3 {
		return nil, fmt.Errorf("lease4-get-all failed: %s", r.Text)
	}
	return parsed[0].Arguments.Leases, nil
}

// StartLease4SyncLoop 周期同步（间隔 30s；失败静默重试）。
func StartLease4SyncLoop(ctx context.Context, pool *pgxpool.Pool, agentURL string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := SyncLease4Bindings(ctx, pool, agentURL); err != nil {
				log.Printf("[lease-sync] %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Lease6 Kea lease6-get-all 行投影（IA_NA 地址租约）。
type Lease6 struct {
	Type          string `json:"type"`
	IPAddress     string `json:"ip-address"`
	DUID          string `json:"duid"`
	Hostname      string `json:"hostname"`
	Cltt          int64  `json:"cltt"`
	ValidLifetime uint32 `json:"valid-lft"`
	State         int    `json:"state"`
}

type keaLease6Resp struct {
	Result    int    `json:"result"`
	Text      string `json:"text"`
	Arguments struct {
		Leases []Lease6 `json:"leases"`
	} `json:"arguments"`
}

// normalizeDUID 归一化客户端标识（小写、去冒号），与 v4 MAC 口径一致。
func normalizeDUID(duid string) string {
	return strings.ToLower(strings.ReplaceAll(duid, ":", ""))
}

// SyncLease6Bindings 拉取 Kea v6 租约并收敛 coherence_binding：v6 行以 DUID 为主键，
// ipv4 落 0.0.0.0 哨兵（与 v4 行 ipv6='::' 哨兵互斥）；清理仅限 ipv4=0.0.0.0 的行。
// 中继场景 v6 无 MAC，DUID 即客户端稳定标识。
func SyncLease6Bindings(ctx context.Context, pool *pgxpool.Pool, agentURL string) error {
	leases, err := fetchLease6(ctx, agentURL)
	if err != nil {
		return err
	}
	seen := make([]string, 0, len(leases))
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, l := range leases {
		if l.Type != "IA_NA" || l.State != 0 || l.DUID == "" || l.IPAddress == "" {
			continue
		}
		duid := normalizeDUID(l.DUID)
		if duid == "" {
			continue
		}
		host := strings.TrimSuffix(l.Hostname, ".")
		if _, err := tx.Exec(ctx,
			`INSERT INTO coherence_binding(mac, ipv4, ipv6, hostname, state, cltt, valid_lft, last_seen)
			 VALUES($1, '0.0.0.0', $2, $3, 'active', $4, $5, now())
			 ON CONFLICT (mac) DO UPDATE SET ipv4='0.0.0.0', ipv6=$2, hostname=$3, state='active',
			   cltt=$4, valid_lft=$5, last_seen=now()`,
			duid, l.IPAddress, host, l.Cltt, int(l.ValidLifetime)); err != nil {
			return err
		}
		seen = append(seen, duid)
	}
	if len(seen) > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM coherence_binding WHERE state='active' AND ipv4='0.0.0.0' AND mac != ALL($1::text[])`,
			seen); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `DELETE FROM coherence_binding WHERE state='active' AND ipv4='0.0.0.0'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `NOTIFY coherence_change`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func fetchLease6(ctx context.Context, agentURL string) ([]Lease6, error) {
	// 同 v4：不带 arguments（空对象会被 Kea 2.2 判为“subnets 未指定”）。
	body, _ := json.Marshal(map[string]any{
		"command": "lease6-get-all",
		"service": []string{"dhcp6"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed []keaLease6Resp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("empty lease6-get-all response")
	}
	if r := parsed[0]; r.Result != 0 && r.Result != 3 {
		return nil, fmt.Errorf("lease6-get-all failed: %s", r.Text)
	}
	return parsed[0].Arguments.Leases, nil
}

// StartLease6SyncLoop 周期同步（间隔 30s；失败静默重试）。
func StartLease6SyncLoop(ctx context.Context, pool *pgxpool.Pool, agentURL string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := SyncLease6Bindings(ctx, pool, agentURL); err != nil {
				log.Printf("[lease6-sync] %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
