package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const policyColumns = `id, name, scope_type, scope_key, priority, severity, notify_mode,
  alert_class, group_by, max_per_hour, repeat_after_sec, resolve_after_sec, enabled,
  created_by, updated_by, created_at, updated_at`

func scanPolicy(sc rowScanner) (Policy, error) {
	var p Policy
	err := sc.Scan(&p.ID, &p.Name, &p.ScopeType, &p.ScopeKey, &p.Priority, &p.Severity,
		&p.NotifyMode, &p.AlertClass, &p.GroupBy, &p.MaxPerHour, &p.RepeatAfterSec,
		&p.ResolveAfterSec, &p.Enabled, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// ListPolicies 查询全部策略。
func (r *PgRepo) ListPolicies(ctx context.Context) ([]Policy, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+policyColumns+" FROM alert_policy ORDER BY scope_type, priority, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Policy, 0, 16)
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePolicy 新增策略。
func (r *PgRepo) CreatePolicy(ctx context.Context, p Policy) (Policy, error) {
	if p.ID == "" {
		p.ID = guuid.NewString()
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	row := r.pool.QueryRow(ctx,
		`INSERT INTO alert_policy(name, scope_type, scope_key, priority, severity, notify_mode,
		 alert_class, group_by, max_per_hour, repeat_after_sec, resolve_after_sec, enabled,
		 created_by, updated_by, created_at, updated_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		 RETURNING `+policyColumns,
		p.Name, p.ScopeType, p.ScopeKey, p.Priority, p.Severity, p.NotifyMode,
		p.AlertClass, p.GroupBy, p.MaxPerHour, p.RepeatAfterSec, p.ResolveAfterSec, p.Enabled,
		p.CreatedBy, p.UpdatedBy, p.CreatedAt, p.UpdatedAt)
	out, err := scanPolicy(row)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return Policy{}, ErrConflict
	}
	return out, err
}

// UpdatePolicy 更新策略。
func (r *PgRepo) UpdatePolicy(ctx context.Context, p Policy) (Policy, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE alert_policy SET name=$2, priority=$3, severity=$4, notify_mode=$5,
		 alert_class=$6, group_by=$7, max_per_hour=$8, repeat_after_sec=$9,
		 resolve_after_sec=$10, enabled=$11, updated_by=$12, updated_at=now()
		 WHERE id=$1 RETURNING `+policyColumns,
		p.ID, p.Name, p.Priority, p.Severity, p.NotifyMode, p.AlertClass, p.GroupBy,
		p.MaxPerHour, p.RepeatAfterSec, p.ResolveAfterSec, p.Enabled, p.UpdatedBy)
	out, err := scanPolicy(row)
	if err == pgx.ErrNoRows {
		return Policy{}, ErrNotFound
	}
	return out, err
}

// DeletePolicy 删除策略。
func (r *PgRepo) DeletePolicy(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM alert_policy WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SeedPolicies 首次落库默认策略（不覆盖用户修改）。
func (r *PgRepo) SeedPolicies(ctx context.Context, ps []Policy) error {
	for _, p := range ps {
		if p.ID == "" {
			p.ID = guuid.NewString()
		}
		now := time.Now().UTC()
		_, err := r.pool.Exec(ctx,
			`INSERT INTO alert_policy(id, name, scope_type, scope_key, priority, severity, notify_mode,
			 alert_class, group_by, max_per_hour, repeat_after_sec, resolve_after_sec, enabled,
			 created_by, updated_by, created_at, updated_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'system','system',$14,$14)
			 ON CONFLICT (scope_type, scope_key) DO NOTHING`,
			p.ID, p.Name, p.ScopeType, p.ScopeKey, p.Priority, p.Severity, p.NotifyMode,
			p.AlertClass, p.GroupBy, p.MaxPerHour, p.RepeatAfterSec, p.ResolveAfterSec, p.Enabled, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// ── 静音 ──

func (r *PgRepo) ListSilences(ctx context.Context) ([]Silence, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, matchers, starts_at, ends_at, reason, created_by, created_at FROM alert_silence ORDER BY starts_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Silence, 0, 8)
	for rows.Next() {
		var s Silence
		var raw []byte
		if err := rows.Scan(&s.ID, &raw, &s.StartsAt, &s.EndsAt, &s.Reason, &s.CreatedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &s.Matchers)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PgRepo) CreateSilence(ctx context.Context, s Silence) (Silence, error) {
	if s.ID == "" {
		s.ID = guuid.NewString()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	raw, _ := json.Marshal(s.Matchers)
	err := r.pool.QueryRow(ctx,
		`INSERT INTO alert_silence(matchers, starts_at, ends_at, reason, created_by, created_at)
		 VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		raw, s.StartsAt, s.EndsAt, s.Reason, s.CreatedBy, s.CreatedAt).Scan(&s.ID)
	return s, err
}

func (r *PgRepo) DeleteSilence(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM alert_silence WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── 通道 ──

func scanChannel(sc rowScanner) (Channel, error) {
	var ch Channel
	var raw []byte
	var lastTest *time.Time
	err := sc.Scan(&ch.ID, &ch.Name, &ch.Type, &raw, &ch.SecretFile, &ch.MinSeverity,
		&ch.QuietHours, &ch.Enabled, &lastTest, &ch.LastStatus, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return ch, err
	}
	ch.LastTestAt = lastTest
	_ = json.Unmarshal(raw, &ch.Config)
	return ch, nil
}

// ListChannels 查询通道。
func (r *PgRepo) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, type, config, secret_file, min_severity, quiet_hours, enabled,
		        last_test_at, last_status, created_at, updated_at
		 FROM notification_channel ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Channel, 0, 4)
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// GetChannel 按 ID 查询通道。
func (r *PgRepo) GetChannel(ctx context.Context, id string) (Channel, bool, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, type, config, secret_file, min_severity, quiet_hours, enabled,
		        last_test_at, last_status, created_at, updated_at
		 FROM notification_channel WHERE id=$1`, id)
	ch, err := scanChannel(row)
	if err == pgx.ErrNoRows {
		return Channel{}, false, nil
	}
	return ch, err == nil, err
}

// CreateChannel 新增通道。
func (r *PgRepo) CreateChannel(ctx context.Context, ch Channel) (Channel, error) {
	if ch.ID == "" {
		ch.ID = guuid.NewString()
	}
	now := time.Now().UTC()
	ch.CreatedAt, ch.UpdatedAt = now, now
	raw, _ := json.Marshal(ch.Config)
	row := r.pool.QueryRow(ctx,
		`INSERT INTO notification_channel(name, type, config, secret_file, min_severity,
		 quiet_hours, enabled, last_status, created_at, updated_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 RETURNING id, name, type, config, secret_file, min_severity, quiet_hours, enabled,
		           last_test_at, last_status, created_at, updated_at`,
		ch.Name, ch.Type, raw, ch.SecretFile, ch.MinSeverity, ch.QuietHours, ch.Enabled,
		ch.LastStatus, ch.CreatedAt, ch.UpdatedAt)
	return scanChannel(row)
}

// UpdateChannel 更新通道。
func (r *PgRepo) UpdateChannel(ctx context.Context, ch Channel) (Channel, error) {
	raw, _ := json.Marshal(ch.Config)
	row := r.pool.QueryRow(ctx,
		`UPDATE notification_channel SET name=$2, type=$3, config=$4, secret_file=$5,
		 min_severity=$6, quiet_hours=$7, enabled=$8, last_test_at=$9, last_status=$10,
		 updated_at=now() WHERE id=$1
		 RETURNING id, name, type, config, secret_file, min_severity, quiet_hours, enabled,
		           last_test_at, last_status, created_at, updated_at`,
		ch.ID, ch.Name, ch.Type, raw, ch.SecretFile, ch.MinSeverity, ch.QuietHours,
		ch.Enabled, ch.LastTestAt, ch.LastStatus)
	out, err := scanChannel(row)
	if err == pgx.ErrNoRows {
		return Channel{}, ErrNotFound
	}
	return out, err
}

// DeleteChannel 删除通道。
func (r *PgRepo) DeleteChannel(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM notification_channel WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateDelivery 记录投递结果。
func (r *PgRepo) CreateDelivery(ctx context.Context, d Delivery) error {
	if d.ID == "" {
		d.ID = guuid.NewString()
	}
	t := d.SentAt
	if t.IsZero() {
		t = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notification_delivery(id, alert_id, channel_id, channel_name, status, attempt, error, sent_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		d.ID, d.AlertID, d.ChannelID, d.ChannelName, d.Status, d.Attempt, d.Error, t)
	return err
}

// ListDeliveries 查询投递记录。
func (r *PgRepo) ListDeliveries(ctx context.Context, f DeliveryFilter) ([]Delivery, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	conds := []string{"1=1"}
	args := []any{}
	if f.ChannelID != "" {
		args = append(args, f.ChannelID)
		conds = append(conds, fmt.Sprintf("channel_id=$%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, fmt.Sprintf("status=$%d", len(args)))
	}
	where := strings.Join(conds, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM notification_delivery WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qargs := append(append([]any{}, args...), f.Limit)
	rows, err := r.pool.Query(ctx,
		`SELECT id, alert_id, channel_id, channel_name, status, attempt, error, sent_at, created_at
		 FROM notification_delivery WHERE `+where+` ORDER BY sent_at DESC LIMIT $`+fmt.Sprint(len(qargs)), qargs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]Delivery, 0, f.Limit)
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.AlertID, &d.ChannelID, &d.ChannelName, &d.Status,
			&d.Attempt, &d.Error, &d.SentAt, &d.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, int(total), rows.Err()
}
