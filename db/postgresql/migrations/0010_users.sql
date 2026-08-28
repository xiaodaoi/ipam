-- 0010: 用户与角色（M5-004，§13.4 系统管理；口令 bcrypt 落库，明文不存储）
CREATE TABLE IF NOT EXISTS users (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username      text NOT NULL UNIQUE,
  display_name  text NOT NULL DEFAULT '',
  password_hash text NOT NULL,
  roles         text[] NOT NULL DEFAULT '{user}',
  enabled       boolean NOT NULL DEFAULT true,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
-- 初始 admin 由应用侧 EnsureBootstrap 播种（口令语义沿用 M5-001：IPAM_POC_PASSWORD/admin123）
