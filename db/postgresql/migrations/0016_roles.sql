-- 0016: RBAC 角色管理（M2-035）——自定义角色 + 内置角色种子
CREATE TABLE IF NOT EXISTS roles (
    name        TEXT PRIMARY KEY,
    permissions TEXT NOT NULL,
    builtin     BOOL NOT NULL DEFAULT false
);
INSERT INTO roles (name, permissions, builtin) VALUES
    ('admin', '["dash:read","dash:write","logs:read","logs:write","dhcp:read","dhcp:write","dns:read","dns:write","system:read","system:write","assets:read","assets:write"]', true),
    ('operator', '["dash:read","logs:read","dhcp:read","dhcp:write","dns:read","dns:write","assets:read","assets:write","system:read"]', true),
    ('auditor', '["dash:read","logs:read","dhcp:read","dns:read","assets:read","system:read"]', true),
    ('user', '["dash:read","logs:read","dhcp:read","dns:read","assets:read"]', true)
ON CONFLICT (name) DO NOTHING;
