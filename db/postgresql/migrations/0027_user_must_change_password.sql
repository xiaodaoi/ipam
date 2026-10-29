-- 0027: 口令强制修改标记（M6-001）
-- 标记账号是否“需改密”（默认 false）。是否真正强制由 security_settings.force_password_change 决定（默认关闭）。
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password boolean NOT NULL DEFAULT false;
