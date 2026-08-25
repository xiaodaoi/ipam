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
			logErr("notify bad payload: %v", uerr)
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
