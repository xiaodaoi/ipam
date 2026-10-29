-- 0028: 账号安全策略（M6-001）——默认关闭强制改密，便于调试/平滑上线。
CREATE TABLE IF NOT EXISTS security_settings (
  id                    smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  force_password_change boolean NOT NULL DEFAULT false,
  updated_at            timestamptz NOT NULL DEFAULT now()
);
INSERT INTO security_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
-- 升级兼容：不因历史标记强制 admin 改密（策略默认关闭）
UPDATE users SET must_change_password = false WHERE username = 'admin';
