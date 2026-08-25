-- 0001_init.sql — §3 核心数据模型（幂等：CI/初始化均直接执行）
-- 联动核心
CREATE TABLE IF NOT EXISTS prefix_template (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name          text NOT NULL,
  ipv4_cidr     cidr NOT NULL,
  ipv6_prefix   cidr NOT NULL,
  encoding      text NOT NULL CHECK (encoding IN ('B','A','CUSTOM')),
  expr          text NOT NULL,
  dns_sync      boolean NOT NULL DEFAULT true,
  grace_hours   int NOT NULL DEFAULT 24,
  enabled       boolean NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS coherence_binding (
  mac            text PRIMARY KEY,
  ipv4           inet NOT NULL,
  ipv6           inet NOT NULL,
  template_id    uuid REFERENCES prefix_template(id),
  hostname       text,
  state          text NOT NULL DEFAULT 'active'
                 CHECK (state IN ('pending','active','grace','expired','conflict')),
  conflict_reason text,
  last_seen      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_binding_ipv6 ON coherence_binding(ipv6);

CREATE TABLE IF NOT EXISTS reservation (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  mac text, duid text,
  ipv4 inet, ipv6 inet[],
  origin text NOT NULL CHECK (origin IN ('manual','coherence','import'))
);

-- 全局组织分组（主数据，§13.4）
CREATE TABLE IF NOT EXISTS org_group (
  id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  parent_id uuid REFERENCES org_group(id) ON DELETE RESTRICT,
  name      text NOT NULL,
  path      text NOT NULL,
  UNIQUE (parent_id, name)
);

-- 资产登记（MAC 为身份键，§13.4）
CREATE TABLE IF NOT EXISTS asset (
  mac        text PRIMARY KEY,
  org_id     uuid REFERENCES org_group(id),
  owner      text,
  dept       text,
  note       text,
  tags       text[] NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- DNS 封禁（FR-B-11~18，D13）
CREATE TABLE IF NOT EXISTS blocklist (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL,
  kind       text NOT NULL CHECK (kind IN ('builtin','custom','feed')),
  sync_url   text,
  last_sync  timestamptz,
  version    int NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS blocklist_entry (
  list_id       uuid REFERENCES blocklist(id) ON DELETE CASCADE,
  trigger_type  text NOT NULL CHECK (trigger_type IN ('qname','response_ip')),
  pattern       text NOT NULL,
  action        text NOT NULL DEFAULT 'nxdomain'
                CHECK (action IN ('nxdomain','drop','tcp_only','redirect')),
  redirect_target text,
  category      text,
  PRIMARY KEY (list_id, trigger_type, pattern)
);

CREATE TABLE IF NOT EXISTS policy_group (
  id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name      text NOT NULL,
  view_name text UNIQUE NOT NULL,
  cidrs     cidr[] NOT NULL,
  list_ids  uuid[] NOT NULL DEFAULT '{}',
  schedule  jsonb
);
