-- 0034: 逐 IP 台账元数据（M6-010B3：VLAN/站点/负责人/用途/备注）
CREATE TABLE IF NOT EXISTS ledger_meta (
  address      inet PRIMARY KEY,
  subnet_id    uuid REFERENCES subnet(id) ON DELETE CASCADE,
  site         text NOT NULL DEFAULT '',
  vlan         int  NOT NULL DEFAULT 0,
  purpose      text NOT NULL DEFAULT '',
  "user"       text NOT NULL DEFAULT '',
  phone        text NOT NULL DEFAULT '',
  remark       text NOT NULL DEFAULT '',
  custom_attrs jsonb NOT NULL DEFAULT '{}'::jsonb,
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ledger_meta_subnet ON ledger_meta(subnet_id);
