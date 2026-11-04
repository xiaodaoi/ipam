-- 0041: 名单版本快照（M6-023 内置库/订阅同步/CSV 导入的可回滚基线）
CREATE TABLE IF NOT EXISTS blocklist_version (
  list_id    uuid NOT NULL REFERENCES blocklist(id) ON DELETE CASCADE,
  version    int  NOT NULL,
  entries    jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (list_id, version)
);
