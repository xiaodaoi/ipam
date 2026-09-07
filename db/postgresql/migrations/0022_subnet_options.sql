-- 0022: 子网级 DHCP 选项（覆盖全局 option-data；space 由 family 推导不入库）。
CREATE TABLE IF NOT EXISTS subnet_option (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subnet_id   uuid NOT NULL REFERENCES subnet(id) ON DELETE CASCADE,
  code        integer NOT NULL,
  name        text,
  data        text NOT NULL,
  csv_format  boolean NOT NULL DEFAULT true,
  enabled     boolean NOT NULL DEFAULT true,
  UNIQUE (subnet_id, code)
);
