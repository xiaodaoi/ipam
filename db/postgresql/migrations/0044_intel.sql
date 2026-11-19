-- 0044: RPZ 化封禁 + 白名单豁免（M6-038 P0）
-- 只存元数据与白名单；zone 内容以磁盘编译产物为准（百万级 IOC 不入库）。

-- 拦截白名单：以 RPZ passthru 下发到 wl.whitelist.rpz，排在所有封禁区之前。
CREATE TABLE IF NOT EXISTS intel_whitelist (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind       text NOT NULL CHECK (kind IN ('qname','suffix')),
  value      text NOT NULL,
  reason     text NOT NULL,
  category   text,
  source     text NOT NULL DEFAULT 'manual'
             CHECK (source IN ('manual','builtin','feed_exclude','auto_fp')),
  expires_at timestamptz,
  enabled    boolean NOT NULL DEFAULT true,
  created_by text,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (kind, value)
);
CREATE INDEX IF NOT EXISTS idx_intel_whitelist_enabled ON intel_whitelist(enabled);

-- RPZ 区运行态快照：serial 为编译写入值，loaded_serial 为 list_auth_zones 观测值，
-- 两者不一致或 status=failed 表示下发异常（UI 据此告警）。
CREATE TABLE IF NOT EXISTS intel_zone (
  zone_name     text PRIMARY KEY,
  entries       bigint NOT NULL DEFAULT 0,
  soa_serial    bigint NOT NULL DEFAULT 0,
  loaded_serial bigint NOT NULL DEFAULT 0,
  sha256        text,
  file_path     text,
  status        text NOT NULL DEFAULT 'empty'
                CHECK (status IN ('empty','loaded','stale','failed')),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
