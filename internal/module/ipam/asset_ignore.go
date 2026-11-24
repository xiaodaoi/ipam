package ipam

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MemIgnoredRepo 忽略名单内存实现（PoC/单测）。
type MemIgnoredRepo struct{ items map[string]IgnoredTerminal }

func NewMemIgnoredRepo() *MemIgnoredRepo {
	return &MemIgnoredRepo{items: map[string]IgnoredTerminal{}}
}

func (r *MemIgnoredRepo) List(_ context.Context) ([]IgnoredTerminal, error) {
	out := make([]IgnoredTerminal, 0, len(r.items))
	for _, it := range r.items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out, nil
}

func (r *MemIgnoredRepo) Upsert(_ context.Context, it IgnoredTerminal) error {
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now().UTC()
	}
	r.items[it.Identity] = it
	return nil
}

func (r *MemIgnoredRepo) Delete(_ context.Context, identity string) error {
	if _, ok := r.items[identity]; ok {
		delete(r.items, identity)
		return nil
	}
	for key, it := range r.items {
		if it.MAC == identity || (it.DUID != "" && it.DUID == identity) {
			delete(r.items, key)
			return nil
		}
	}
	return ErrIgnoredNotFound
}

// PgIgnoredRepo 忽略名单 PG 实现（表 asset_ignore，迁移 0052）。
type PgIgnoredRepo struct{ pool *pgxpool.Pool }

func NewPgIgnoredRepo(pool *pgxpool.Pool) *PgIgnoredRepo { return &PgIgnoredRepo{pool: pool} }

const ignoredCols = `identity, coalesce(mac,''), coalesce(duid,''), coalesce(reason,''), created_at`

func (r *PgIgnoredRepo) List(ctx context.Context) ([]IgnoredTerminal, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+ignoredCols+` FROM asset_ignore ORDER BY identity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IgnoredTerminal{}
	for rows.Next() {
		var it IgnoredTerminal
		if err := rows.Scan(&it.Identity, &it.MAC, &it.DUID, &it.Reason, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *PgIgnoredRepo) Upsert(ctx context.Context, it IgnoredTerminal) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO asset_ignore(identity, mac, duid, reason, created_at)
		 VALUES($1,$2,$3,$4,now())
		 ON CONFLICT (identity) DO UPDATE SET
		   mac=coalesce(EXCLUDED.mac, asset_ignore.mac),
		   duid=coalesce(EXCLUDED.duid, asset_ignore.duid),
		   reason=EXCLUDED.reason`,
		it.Identity, nullStr(it.MAC), nullStr(it.DUID), nullStr(it.Reason))
	return err
}

func (r *PgIgnoredRepo) Delete(ctx context.Context, identity string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM asset_ignore WHERE identity=$1 OR mac=$1 OR duid=$1`, identity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrIgnoredNotFound
	}
	return nil
}

// 编译期断言：两套实现满足接口。
var (
	_ IgnoredRepo = (*PgIgnoredRepo)(nil)
	_ IgnoredRepo = (*MemIgnoredRepo)(nil)
)
