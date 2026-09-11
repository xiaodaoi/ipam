-- 0025: 双栈关联匹配方案（M3-013，§4.5）——模板级 matchScheme + MAC↔DUID 映射/冲突表。
ALTER TABLE prefix_template ADD COLUMN IF NOT EXISTS match_scheme text NOT NULL DEFAULT 'auto';
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'prefix_template_match_scheme_chk') THEN
    ALTER TABLE prefix_template ADD CONSTRAINT prefix_template_match_scheme_chk
      CHECK (match_scheme IN ('auto','option79','client-id','duid-llt','hostname','admin'));
  END IF;
END $$;

-- 人工映射（source=admin）与精确方式自动学习（source=auto）；hostname 启发式不落表。
CREATE TABLE IF NOT EXISTS dualstack_identity (
  mac        text PRIMARY KEY,
  duid       text NOT NULL UNIQUE,
  source     text NOT NULL DEFAULT 'admin' CHECK (source IN ('admin','auto')),
  note       text,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ds_identity_duid ON dualstack_identity(duid);

-- 对账器每周期覆盖写入的未解析项（同名歧义/无 MAC 信号/钉死回落）。
CREATE TABLE IF NOT EXISTS dualstack_conflict (
  duid       text PRIMARY KEY,
  v6_ip      text,
  v4_macs    text[],
  hostname   text,
  reason     text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
