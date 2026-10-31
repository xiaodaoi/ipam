-- 0036: TOTP 恢复码（M6-007A）：仅存 SHA-256，用后即删。
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_recovery text[] NOT NULL DEFAULT '{}';
