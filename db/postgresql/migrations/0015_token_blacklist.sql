-- 0015: 令牌黑名单持久化（M5-012）——登出即吊销跨重启有效
CREATE TABLE IF NOT EXISTS auth_token_blacklist (
    token_hash TEXT PRIMARY KEY,
    until      TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_token_blacklist_until ON auth_token_blacklist (until);
