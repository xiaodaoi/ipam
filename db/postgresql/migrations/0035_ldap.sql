-- 0035: LDAP/AD 目录服务（M6-007B）
ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_source text NOT NULL DEFAULT 'local';

CREATE TABLE IF NOT EXISTS ldap_settings (
  id                   smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  enabled              boolean NOT NULL DEFAULT false,
  server_url           text NOT NULL DEFAULT 'ldap://127.0.0.1:389',
  start_tls            boolean NOT NULL DEFAULT false,
  insecure_skip_verify boolean NOT NULL DEFAULT false,
  bind_dn              text NOT NULL DEFAULT '',
  bind_password        text NOT NULL DEFAULT '',
  base_dn              text NOT NULL DEFAULT '',
  user_filter          text NOT NULL DEFAULT '(uid={username})',
  group_base_dn        text NOT NULL DEFAULT '',
  group_filter         text NOT NULL DEFAULT '(&(objectClass=groupOfNames)(member={dn}))',
  group_attr           text NOT NULL DEFAULT 'cn',
  role_map             jsonb NOT NULL DEFAULT '{}'::jsonb,
  default_role         text NOT NULL DEFAULT 'user',
  updated_at           timestamptz NOT NULL DEFAULT now()
);
INSERT INTO ldap_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
