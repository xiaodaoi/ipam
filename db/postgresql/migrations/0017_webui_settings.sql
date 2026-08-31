-- 0017: Web 页面设置（M2-039）——单行表（siteName/faviconUrl/logoUrl）
CREATE TABLE IF NOT EXISTS webui_settings (
    id          BOOL PRIMARY KEY DEFAULT true CHECK (id),
    site_name   TEXT NOT NULL DEFAULT '',
    favicon_url TEXT NOT NULL DEFAULT '',
    logo_url    TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO webui_settings (id) VALUES (true) ON CONFLICT DO NOTHING;
