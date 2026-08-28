package coherence

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TplLoader 从 PG prefix_template 周期装载启用模板（M2-013，§4.3 多池对）。
// 缓存 + 互斥：Resolve 路径高频读 All()/Lookup()，不逐次打库。
type TplLoader struct {
	pool *pgxpool.Pool

	mu   sync.RWMutex
	tpls []Template
}

func NewTplLoader(pool *pgxpool.Pool) *TplLoader {
	return &TplLoader{pool: pool}
}

// tplRow prefix_template 查询行（纯结构，便于单测投影逻辑）。
type tplRow struct {
	ID     string
	V4Cidr string // PG cidr::text，如 "192.168.0.0/24"
	V6Pre  string // PG cidr::text，如 "2407::/64"
	Expr   string
}

// projectTemplate 行 → 联动模板投影；v6 前缀归一化为 "2407::"（ApplyTemplate 拼接约定）。
// v6 容忍裸地址形态（"2406::" 与 "2406::/64" 等价处理）；v4 严格要求 CIDR。
func projectTemplate(r tplRow) (Template, bool) {
	prefix := strings.TrimSpace(r.V6Pre)
	p, err := netip.ParsePrefix(prefix)
	if err != nil {
		addr, aerr := netip.ParseAddr(prefix)
		if aerr != nil {
			return Template{}, false
		}
		p = netip.PrefixFrom(addr, addr.BitLen())
	}
	v4, err := netip.ParsePrefix(strings.TrimSpace(r.V4Cidr))
	if err != nil {
		return Template{}, false
	}
	return Template{
		ID:     r.ID,
		V4Cidr: v4.String(),
		Prefix: p.Addr().String(),
		Expr:   r.Expr,
	}, true
}

// Refresh 全量拉取 enabled 模板；失败保留旧缓存（对账期间 PG 抖动不致联动瘫痪）。
func (l *TplLoader) Refresh(ctx context.Context) (int, error) {
	rows, err := l.pool.Query(ctx,
		`SELECT id::text, ipv4_cidr::text, ipv6_prefix::text, expr
		 FROM prefix_template WHERE enabled`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		var r tplRow
		if err := rows.Scan(&r.ID, &r.V4Cidr, &r.V6Pre, &r.Expr); err != nil {
			return 0, err
		}
		if t, ok := projectTemplate(r); ok {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	l.mu.Lock()
	l.tpls = out
	l.mu.Unlock()
	return len(out), nil
}

// All 当前缓存模板快照（拷贝，防调用方改写）。
func (l *TplLoader) All() []Template {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Template, len(l.tpls))
	copy(out, l.tpls)
	return out
}

// Lookup 按 ID 查缓存（TemplateLookup 适配）。
func (l *TplLoader) Lookup(id string) (Template, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, t := range l.tpls {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// StartRefreshLoop 周期刷新直至 ctx 取消；间隔与 Reconciler 对账节奏同量级。
func (l *TplLoader) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := l.Refresh(ctx)
				if err != nil && ctx.Err() == nil {
					continue
				}
				_ = n
			}
		}
	}()
}
