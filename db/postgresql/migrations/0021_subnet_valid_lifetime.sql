-- 0021: 子网级租约时长（valid_lifetime 秒）；0/缺省回落 Kea 全局 3600。
ALTER TABLE subnet ADD COLUMN IF NOT EXISTS valid_lifetime integer NOT NULL DEFAULT 3600;
