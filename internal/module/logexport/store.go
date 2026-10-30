package logexport

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MemRepo struct{ s Settings }

func NewMemRepo() *MemRepo { return &MemRepo{s: Defaults()} }

func (m *MemRepo) Get(context.Context) (Settings, error) { return m.s, nil }
func (m *MemRepo) Save(_ context.Context, s Settings) (Settings, error) {
	s.UpdatedAt = time.Now().UTC()
	m.s = s
	return s, nil
}

type PgRepo struct{ pool *pgxpool.Pool }

func NewPgRepo(pool *pgxpool.Pool) *PgRepo { return &PgRepo{pool: pool} }

func (p *PgRepo) Get(ctx context.Context) (Settings, error) {
	var s Settings
	err := p.pool.QueryRow(ctx,
		`SELECT syslog_enabled, syslog_address, syslog_mode, kafka_enabled, kafka_brokers, kafka_topic,
		        webhook_enabled, webhook_url, sampling_mode, sample_rate, updated_at
		   FROM log_export_settings WHERE id=1`).
		Scan(&s.SyslogEnabled, &s.SyslogAddress, &s.SyslogMode, &s.KafkaEnabled, &s.KafkaBrokers,
			&s.KafkaTopic, &s.WebhookEnabled, &s.WebhookURL, &s.SamplingMode, &s.SampleRate, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Defaults(), nil
	}
	return s, err
}

func (p *PgRepo) Save(ctx context.Context, s Settings) (Settings, error) {
	var out Settings
	err := p.pool.QueryRow(ctx,
		`INSERT INTO log_export_settings(id, syslog_enabled, syslog_address, syslog_mode, kafka_enabled,
		    kafka_brokers, kafka_topic, webhook_enabled, webhook_url, sampling_mode, sample_rate, updated_at)
		 VALUES(1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
		 ON CONFLICT (id) DO UPDATE SET syslog_enabled=EXCLUDED.syslog_enabled,
		   syslog_address=EXCLUDED.syslog_address, syslog_mode=EXCLUDED.syslog_mode,
		   kafka_enabled=EXCLUDED.kafka_enabled, kafka_brokers=EXCLUDED.kafka_brokers,
		   kafka_topic=EXCLUDED.kafka_topic, webhook_enabled=EXCLUDED.webhook_enabled,
		   webhook_url=EXCLUDED.webhook_url, sampling_mode=EXCLUDED.sampling_mode,
		   sample_rate=EXCLUDED.sample_rate, updated_at=now()
		 RETURNING syslog_enabled, syslog_address, syslog_mode, kafka_enabled, kafka_brokers,
		   kafka_topic, webhook_enabled, webhook_url, sampling_mode, sample_rate, updated_at`,
		s.SyslogEnabled, s.SyslogAddress, s.SyslogMode, s.KafkaEnabled, s.KafkaBrokers,
		s.KafkaTopic, s.WebhookEnabled, s.WebhookURL, s.SamplingMode, s.SampleRate).
		Scan(&out.SyslogEnabled, &out.SyslogAddress, &out.SyslogMode, &out.KafkaEnabled,
			&out.KafkaBrokers, &out.KafkaTopic, &out.WebhookEnabled, &out.WebhookURL,
			&out.SamplingMode, &out.SampleRate, &out.UpdatedAt)
	return out, err
}

var _ = time.Now
