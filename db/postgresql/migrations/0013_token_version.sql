-- 0013: 令牌吊销（M5-010）：用户级会话版本号——改密/停用/角色变更时 +1，存量令牌立即失效
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version int NOT NULL DEFAULT 1;
