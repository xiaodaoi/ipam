-- 0018: Web 页面设置端口（M2-046）——server_port 可编辑，重启后生效
ALTER TABLE webui_settings ADD COLUMN IF NOT EXISTS server_port INTEGER NOT NULL DEFAULT 8443;