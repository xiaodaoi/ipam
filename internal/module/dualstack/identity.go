// identity.go MAC↔DUID 映射与冲突清单（M3-013，§4.5）。
package dualstack

import (
	"context"
	"time"
)

// Identity MAC↔DUID 映射行（dualstack_identity 表投影）。
type Identity struct {
	Mac       string    `json:"mac"`
	Duid      string    `json:"duid"`
	Source    string    `json:"source"` // admin | auto
	Note      string    `json:"note,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Conflict 未解析冲突项（dualstack_conflict 表投影）。
type Conflict struct {
	Duid      string    `json:"duid"`
	V6Ip      string    `json:"v6Ip,omitempty"`
	V4Macs    []string  `json:"v4Macs,omitempty"`
	Hostname  string    `json:"hostname,omitempty"`
	Reason    string    `json:"reason"` // ambiguous_hostname | no_mac_signal | pinned_mismatch
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListIdentities 查询全部映射（人工 + 自动学习）。
func (s *PgStore) ListIdentities(ctx context.Context) ([]Identity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT mac, duid, source, coalesce(note,''), updated_at FROM dualstack_identity ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var it Identity
		if err := rows.Scan(&it.Mac, &it.Duid, &it.Source, &it.Note, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UpsertIdentity 新建/覆盖映射（mac 主键冲突覆盖；duid 唯一冲突时先清同 duid 旧行）。
func (s *PgStore) UpsertIdentity(ctx context.Context, mac, duid, source, note string) (Identity, error) {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM dualstack_identity WHERE duid=$1 AND mac<>$2`, duid, mac); err != nil {
		return Identity{}, err
	}
	var it Identity
	err := s.pool.QueryRow(ctx,
		`INSERT INTO dualstack_identity(mac, duid, source, note, updated_at)
		 VALUES($1,$2,$3,$4,now())
		 ON CONFLICT (mac) DO UPDATE SET duid=$2, source=$3, note=$4, updated_at=now()
		 RETURNING mac, duid, source, coalesce(note,''), updated_at`,
		mac, duid, source, note).Scan(&it.Mac, &it.Duid, &it.Source, &it.Note, &it.UpdatedAt)
	return it, err
}

// DeleteIdentity 删除映射。
func (s *PgStore) DeleteIdentity(ctx context.Context, mac string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM dualstack_identity WHERE mac=$1`, mac)
	return err
}

// ListConflicts 查询冲突清单。
func (s *PgStore) ListConflicts(ctx context.Context) ([]Conflict, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT duid, coalesce(v6_ip,''), coalesce(v4_macs,'{}'), coalesce(hostname,''), reason, updated_at
		 FROM dualstack_conflict ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conflict{}
	for rows.Next() {
		var cf Conflict
		if err := rows.Scan(&cf.Duid, &cf.V6Ip, &cf.V4Macs, &cf.Hostname, &cf.Reason, &cf.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, cf)
	}
	return out, rows.Err()
}
