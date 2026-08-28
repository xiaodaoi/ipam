-- 0011: DHCP 选项与类匹配（M2-016，C-02/C-03；Kea option-data/client-classes 投影）
CREATE TABLE IF NOT EXISTS dhcp_options (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  option_code int NOT NULL,
  name        text NOT NULL,
  data        text NOT NULL,
  enabled     boolean NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE(option_code, name)
);
CREATE TABLE IF NOT EXISTS dhcp_classes (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL UNIQUE,
  test       text NOT NULL DEFAULT '',
  options    jsonb NOT NULL DEFAULT '[]',
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
