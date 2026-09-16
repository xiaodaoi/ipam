-- 0026: 日志存储策略 + 按月归档（日志生命周期管理）
-- 存储策略单行表：ClickHouse TTL 天数 / 归档保留月数 / 自动导出开关
CREATE TABLE IF NOT EXISTS log_settings (
    id                  BOOL PRIMARY KEY DEFAULT true CHECK (id),
    retention_days      INT  NOT NULL DEFAULT 180,
    archive_keep_months INT  NOT NULL DEFAULT 12,
    auto_export         BOOL NOT NULL DEFAULT true,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO log_settings (id) VALUES (true) ON CONFLICT DO NOTHING;

-- 按月归档元数据：Parquet 文件名/大小/校验/来源
CREATE TABLE IF NOT EXISTS log_archive (
    month       DATE PRIMARY KEY,              -- 当月 1 日，如 2026-01-01
    row_count   BIGINT NOT NULL DEFAULT 0,
    file_size   BIGINT NOT NULL DEFAULT 0,
    file_path   TEXT   NOT NULL DEFAULT '',
    file_hash   TEXT   NOT NULL DEFAULT '',
    exported_by TEXT   NOT NULL DEFAULT 'system',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
