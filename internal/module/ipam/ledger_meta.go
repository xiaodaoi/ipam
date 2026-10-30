package ipam

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LedgerMeta 逐 IP 台账元数据（VLAN/站点/负责人/用途/备注）。
type LedgerMeta struct {
	Address     string
	SubnetID    string
	Site        string
	VLAN        int
	Purpose     string
	User        string
	Phone       string
	Remark      string
	CustomAttrs map[string]string
}

// LedgerMetaRepo 元数据持久化。
type LedgerMetaRepo interface {
	List(ctx context.Context, subnetID string) (map[string]LedgerMeta, error)
	Upsert(ctx context.Context, m LedgerMeta) error
	Delete(ctx context.Context, address string) error
}

// MemLedgerMetaRepo 内存实现（PoC/单测）。
type MemLedgerMetaRepo struct{ m map[string]LedgerMeta }

func NewMemLedgerMetaRepo() *MemLedgerMetaRepo {
	return &MemLedgerMetaRepo{m: map[string]LedgerMeta{}}
}

func (r *MemLedgerMetaRepo) List(_ context.Context, subnetID string) (map[string]LedgerMeta, error) {
	out := map[string]LedgerMeta{}
	for k, v := range r.m {
		if subnetID == "" || v.SubnetID == subnetID {
			out[k] = v
		}
	}
	return out, nil
}

func (r *MemLedgerMetaRepo) Upsert(_ context.Context, m LedgerMeta) error {
	r.m[m.Address] = m
	return nil
}

func (r *MemLedgerMetaRepo) Delete(_ context.Context, address string) error {
	delete(r.m, address)
	return nil
}

// PgLedgerMetaRepo PostgreSQL 实现。
type PgLedgerMetaRepo struct{ pool *pgxpool.Pool }

func NewPgLedgerMetaRepo(pool *pgxpool.Pool) *PgLedgerMetaRepo {
	return &PgLedgerMetaRepo{pool: pool}
}

func (r *PgLedgerMetaRepo) List(ctx context.Context, subnetID string) (map[string]LedgerMeta, error) {
	q := `SELECT host(address), coalesce(subnet_id::text,''), site, vlan, purpose, "user", phone, remark, custom_attrs
	        FROM ledger_meta`
	var args []any
	if subnetID != "" {
		q += ` WHERE subnet_id=$1`
		args = append(args, subnetID)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LedgerMeta{}
	for rows.Next() {
		var m LedgerMeta
		var raw []byte
		if err := rows.Scan(&m.Address, &m.SubnetID, &m.Site, &m.VLAN, &m.Purpose, &m.User, &m.Phone, &m.Remark, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m.CustomAttrs)
		}
		out[m.Address] = m
	}
	return out, rows.Err()
}

func (r *PgLedgerMetaRepo) Upsert(ctx context.Context, m LedgerMeta) error {
	attrs, _ := json.Marshal(nonNilAttrs(m.CustomAttrs))
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ledger_meta(address, subnet_id, site, vlan, purpose, "user", phone, remark, custom_attrs, updated_at)
		VALUES($1::inet, nullif($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (address) DO UPDATE SET subnet_id=EXCLUDED.subnet_id, site=EXCLUDED.site,
		  vlan=EXCLUDED.vlan, purpose=EXCLUDED.purpose, "user"=EXCLUDED."user", phone=EXCLUDED.phone,
		  remark=EXCLUDED.remark, custom_attrs=EXCLUDED.custom_attrs, updated_at=now()`,
		m.Address, m.SubnetID, m.Site, m.VLAN, m.Purpose, m.User, m.Phone, m.Remark, attrs)
	return err
}

func (r *PgLedgerMetaRepo) Delete(ctx context.Context, address string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM ledger_meta WHERE address=$1::inet`, address)
	return err
}

// LoadLedgerMeta 全量加载（台账查询注入）。
func LoadLedgerMeta(ctx context.Context, pool *pgxpool.Pool) (map[string]LedgerMeta, error) {
	return NewPgLedgerMetaRepo(pool).List(ctx, "")
}

func nonNilAttrs(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
