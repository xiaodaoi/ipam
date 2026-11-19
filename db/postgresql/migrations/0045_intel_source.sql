-- 0045: 情报源管理（M6-039 P1 先行：预设默认关闭 + 用户维护）
-- 只存元数据；拉取到的域名以磁盘缓存为准（百万级 IOC 不入库）。
-- 密钥走文件（/run/ipam/intel-secrets，0600），库里只存路径，满足「密钥不入库」红线。

CREATE TABLE IF NOT EXISTS intel_source (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  key              text NOT NULL UNIQUE,           -- 预设键或 custom:<uuid>
  name             text NOT NULL,
  kind             text NOT NULL DEFAULT 'preset' CHECK (kind IN ('preset','custom')),
  layer            text NOT NULL DEFAULT 'hot'    CHECK (layer IN ('hot','broad')),
  url              text NOT NULL DEFAULT '',
  format           text NOT NULL DEFAULT 'auto',
  auth_header      text NOT NULL DEFAULT '',
  secret_file      text NOT NULL DEFAULT '',
  license          text NOT NULL DEFAULT '',
  license_url      text NOT NULL DEFAULT '',
  commercial_ok    boolean NOT NULL DEFAULT false,
  requires_ack     boolean NOT NULL DEFAULT true,
  acknowledged_at  timestamptz,
  update_interval  int NOT NULL DEFAULT 21600,    -- 秒
  enabled          boolean NOT NULL DEFAULT false,
  entries          bigint NOT NULL DEFAULT 0,
  last_sync_at     timestamptz,
  last_status      text NOT NULL DEFAULT 'never' CHECK (last_status IN ('never','ok','failed')),
  last_error       text NOT NULL DEFAULT '',
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_intel_source_enabled ON intel_source(enabled);
