package coherence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgBinding 行投影（coherence_binding 表）。
type pgBinding struct {
	MAC        string  `json:"mac"`
	IPv4       string  `json:"ipv4"`
	IPv6       string  `json:"ipv6"`
	TemplateID *string `json:"template_id"`
	Hostname   *string `json:"hostname"`
	State      string  `json:"state"`
}

func (p pgBinding) toBinding() Binding {
	b := Binding{MAC: p.MAC, IPv4: p.IPv4, IPv6: p.IPv6, Hostname: deref(p.Hostname)}
	if p.TemplateID != nil {
		b.TemplateID = *p.TemplateID
	}
	return b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// LoadAllBindings 启动时全量加载 active 绑定（§2.3 对账：启动全量重放）。
func LoadAllBindings(ctx context.Context, pool *pgxpool.Pool) ([]Binding, error) {
	rows, err := pool.Query(ctx,
		`SELECT mac, host(ipv4), coalesce(host(ipv6),''), template_id, hostname
		 FROM coherence_binding WHERE state IN ('active','grace')`)
	if err != nil {
		return nil, fmt.Errorf("load bindings: %w", err)
	}
	defer rows.Close()

	var out []Binding
	for rows.Next() {
		var mac, ipv4, ipv6 string
		var tpl, host *string
		if err := rows.Scan(&mac, &ipv4, &ipv6, &tpl, &host); err != nil {
			return nil, err
		}
		out = append(out, Binding{MAC: mac, IPv4: ipv4, IPv6: ipv6, TemplateID: deref(tpl), Hostname: deref(host)})
	}
	return out, rows.Err()
}

// reloadable 支持全量枚举的 store（MemStore）；空载荷 NOTIFY 全量重载需要 All()。
type reloadable interface {
	Store
	All() []Binding
}

// ReloadStore 从 PG 全量重载绑定到 store：Put 全量行 + Delete 库中已消失的行。
// 供空载荷 NOTIFY（租约同步等批量写方）触发，替代逐行增量。
func ReloadStore(ctx context.Context, pool *pgxpool.Pool, store *MemStore) error {
	bindings, err := LoadAllBindings(ctx, pool)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		store.Put(b)
		seen[b.MAC] = true
	}
	for _, b := range store.All() {
		if !seen[b.MAC] {
			store.Delete(b.MAC)
		}
	}
	return nil
}

// SubscribeNotify 阻塞订阅 coherence_change 频道，将增量应用到 store。
// 断线自动重连（间隔 5s）；ctx 取消即退出。
func SubscribeNotify(ctx context.Context, pool *pgxpool.Pool, store Store) error {
	for {
		if err := listenOnce(ctx, pool, store); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logErr("notify subscribe broken, retry in 5s: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, store Store) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err = conn.Exec(ctx, "LISTEN coherence_change"); err != nil {
		return err
	}
	var lastReload time.Time
	var lastCount = -1
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var payload struct {
			Op  string    `json:"op"`
			Row pgBinding `json:"row"`
		}
		if uerr := json.Unmarshal([]byte(n.Payload), &payload); uerr != nil {
			// 空载荷/异构 NOTIFY（租约同步等批量写方）：节流全量重载，行数变化才记日志
			if rl, ok := store.(reloadable); ok && time.Since(lastReload) >= 2*time.Second {
				lastReload = time.Now()
				if rerr := ReloadStore(ctx, pool, rl.(*MemStore)); rerr != nil {
					logErr("notify full reload: %v", rerr)
				} else if n := len(rl.All()); n != lastCount {
					logErr("notify full reload: %d bindings", n)
					lastCount = n
				}
			}
			continue
		}
		switch payload.Op {
		case "upsert":
			store.Put(payload.Row.toBinding())
		case "delete":
			store.Delete(payload.Row.MAC)
		}
	}
}
