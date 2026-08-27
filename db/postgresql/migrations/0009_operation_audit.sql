-- 0009_operation_audit.sql — 管理员操作审计（M4-003，FR-F、§12.3 人/Bot 区分）
-- 仅变更类请求入库；token 身份（token_sub）在 M5 JWT 接入后填充，当前为空串
CREATE TABLE IF NOT EXISTS operation_audit (
  id         bigserial PRIMARY KEY,
  ts         timestamptz NOT NULL DEFAULT now(),
  actor_type text NOT NULL CHECK (actor_type IN ('human','bot','system')),
  actor      text NOT NULL DEFAULT 'anonymous',
  token_sub  text NOT NULL DEFAULT '',
  method     text NOT NULL,
  path       text NOT NULL,
  action     text NOT NULL,
  resource   text NOT NULL,
  status     int NOT NULL,
  detail     text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_operation_audit_ts ON operation_audit(ts DESC);