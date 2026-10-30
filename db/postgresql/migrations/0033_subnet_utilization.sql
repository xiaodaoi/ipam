-- 0033: 子网池利用率采样（M6-010B）
CREATE TABLE IF NOT EXISTS subnet_utilization (
  id         bigserial PRIMARY KEY,
  subnet_id  uuid NOT NULL REFERENCES subnet(id) ON DELETE CASCADE,
  ts         timestamptz NOT NULL,
  used       int NOT NULL CHECK (used >= 0),
  capacity   int NOT NULL CHECK (capacity >= 0),
  pct        numeric(6,2) NOT NULL CHECK (pct >= 0 AND pct <= 100)
);
CREATE INDEX IF NOT EXISTS idx_subnet_utilization_subnet_ts ON subnet_utilization(subnet_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_subnet_utilization_ts ON subnet_utilization(ts);
