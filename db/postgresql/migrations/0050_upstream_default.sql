-- 0050: DNS 上游默认公网出口标记（M6-041）
-- 默认转发 "." 由「第一条 enabled 上游」改为「所有 enabled 且 is_default 的上游」，
-- 从而让公网解析具备多地址冗余；未标记任何默认时仍回退第一条 enabled（兼容旧部署）。
-- 仅允许公网地址标记为默认（应用层校验），内网 DNS 继续只能通过条件转发规则使用。

ALTER TABLE upstream
  ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN upstream.is_default IS
  '默认公网出口：enabled 且 is_default 的上游共同下发 forward-zone "."（地址必须为公网地址）';
