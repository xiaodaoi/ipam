-- 0008_settings.sql — DNS 行为参数（缓存/安全，FR-B-06/08/10、F-R2/R3）
CREATE TABLE IF NOT EXISTS dns_settings (
  key   text PRIMARY KEY,   -- 'cache' | 'security'
  value jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS dns_ttl_override (
  domain text PRIMARY KEY,
  ttl    int NOT NULL CHECK (ttl BETWEEN 1 AND 86400)
);
