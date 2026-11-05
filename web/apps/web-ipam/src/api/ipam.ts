import type { components } from './schema';

import { useAccessStore } from '@vben/stores';

/**
 * IPAM 控制面 API 类型化客户端（§13.2：统一走生成的 schema 类型，禁散装 fetch）。
 * 运行时走 Vben 环境 VITE_GLOB_API_URL（dev 代理 /api → :8443）。
 */
export type LedgerRow = components['schemas']['LedgerRow'];
export type LedgerPage = components['schemas']['LedgerPage'];
export type OrgTreeNode = components['schemas']['OrgTreeNode'];
export type Subnet = components['schemas']['Subnet'];
export type SubnetOption = components['schemas']['SubnetOption'];
export type Asset = components['schemas']['Asset'];
export type AssetUpsert = components['schemas']['AssetUpsert'];
export type DiscoveredTerminal = components['schemas']['DiscoveredTerminal'];
export type LedgerMetaUpsert = components['schemas']['LedgerMetaUpsert'];

const BASE = (import.meta.env.VITE_GLOB_API_URL as string) || '/api/v1';

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  // 与 request.ts 同构：请求期取 accessStore（pinia 已装），携带 bearer
  const token = useAccessStore().accessToken;
  const headers = new Headers(init?.headers);
  headers.set('Content-Type', 'application/json');
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const res = await fetch(`${BASE}${path}`, { ...init, headers });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(
      (body as { code?: string; detail?: string }).detail ||
        `HTTP ${res.status}`,
    );
  }
  // 204 等空 body 响应（reserve/bind/release/updateBinding）直接返回 undefined
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const listLedger = (params: {
  cursor?: string;
  family?: number;
  orgId?: string;
  pageSize?: number;
  state?: string;
  subnetId?: string;
}) => {
  const qs = new URLSearchParams();
  if (params.orgId) qs.set('orgId', params.orgId);
  if (params.subnetId) qs.set('subnetId', params.subnetId);
  if (params.family) qs.set('family', String(params.family));
  if (params.state) qs.set('state', params.state);
  if (params.cursor) qs.set('cursor', params.cursor);
  if (params.pageSize) qs.set('pageSize', String(params.pageSize));
  const q = qs.toString();
  return req<LedgerPage>(`/ledger${q ? `?${q}` : ''}`);
};

export const listOrgTree = () => req<OrgTreeNode[]>('/orgs');
export const reorderOrgs = (parentId: null | string, orderedIds: string[]) =>
  req<undefined>('/orgs/reorder', j({ parentId, orderedIds }));

export const reserveAddress = (subnetId: string, address: string) =>
  req<undefined>('/ledger', {
    method: 'POST',
    body: JSON.stringify({ subnetId, address }),
  });

export const bindStatic = (subnetId: string, address: string, mac: string) =>
  req<undefined>('/ledger/bind', {
    method: 'POST',
    body: JSON.stringify({ subnetId, address, mac }),
  });

export const updateBinding = (address: string, mac: string) =>
  req<undefined>('/ledger/bind', {
    method: 'PUT',
    body: JSON.stringify({ address, mac }),
  });

export const releaseAddress = (address: string) =>
  req<undefined>('/ledger/release', {
    method: 'POST',
    body: JSON.stringify({ address }),
  });
export const upsertLedgerMeta = (b: LedgerMetaUpsert) =>
  req<LedgerMetaUpsert>('/ledger/meta', {
    method: 'PUT',
    body: JSON.stringify(b),
  });

export const listSubnets = (orgId?: string) =>
  req<{ items: Subnet[] }>(`/subnets${orgId ? `?orgId=${orgId}` : ''}`);

export const listAssets = (params?: { orgId?: string; q?: string }) => {
  const qs = new URLSearchParams();
  if (params?.orgId) qs.set('orgId', params.orgId);
  if (params?.q) qs.set('q', params.q);
  const s = qs.toString();
  return req<{ items: Asset[]; total?: number }>(`/assets${s ? `?${s}` : ''}`);
};
export const upsertAsset = (b: AssetUpsert) =>
  req<Asset>('/assets', { method: 'PUT', body: JSON.stringify(b) });
export const deleteAsset = (mac: string) =>
  req<undefined>(`/assets/${encodeURIComponent(mac)}`, { method: 'DELETE' });
export const listDiscoveredTerminals = () =>
  req<{ items: DiscoveredTerminal[] }>('/assets/discovered');
export type DashboardOverview = components['schemas']['DashboardOverview'];
export type PoolUtilization = components['schemas']['PoolUtilization'];

export const getDashboard = (poolTopN?: number) =>
  req<DashboardOverview>(
    `/dashboard${poolTopN ? `?poolTopN=${poolTopN}` : ''}`,
  );

export type HostMetrics = components['schemas']['HostMetrics'];
export const getSystemMetrics = () => req<HostMetrics>('/system/metrics');

// ── 主机资源历史（M6-032）──
export type SystemMetricPoint = components['schemas']['SystemMetricPoint'];
export type SystemMetricHistory = components['schemas']['SystemMetricHistory'];
export type SystemMetricSummary = components['schemas']['SystemMetricSummary'];
export const getSystemMetricsHistory = (params: {
  from: string;
  step: number;
  to: string;
}) =>
  req<SystemMetricHistory>(
    `/system/metrics/history?from=${encodeURIComponent(params.from)}&to=${encodeURIComponent(params.to)}&step=${params.step}`,
  );
export const getSystemMetricsSummary = (hours: number) =>
  req<SystemMetricSummary>(`/system/metrics/summary?hours=${hours}`);

export type SystemInfo = components['schemas']['SystemInfo'];
export const getSystemInfo = () => req<SystemInfo>('/system/info');

// ── 账号安全策略（M6-001）──
// ── 日志外发与采样（M6-008）──
export type LogExportSettings = components['schemas']['LogExportSettings'];
export const getLogExportSettings = () =>
  req<LogExportSettings>('/system/log-export-settings');
export const updateLogExportSettings = (s: LogExportSettings) =>
  req<LogExportSettings>('/system/log-export-settings', {
    method: 'PUT',
    body: JSON.stringify(s),
  });

// ── License（M6-004）──
export type LicenseStatus = components['schemas']['LicenseStatus'];
export const getLicenseStatus = () => req<LicenseStatus>('/system/license');
export const activateLicense = (license: string) =>
  req<LicenseStatus>('/system/license', {
    method: 'PUT',
    body: JSON.stringify({ license }),
  });
export const removeLicense = () =>
  req<undefined>('/system/license', { method: 'DELETE' });

// ── 备份与恢复（M6-002）──
export type BackupRecord = components['schemas']['Backup'];
export type BackupSettings = components['schemas']['BackupSettings'];
export const listBackups = () =>
  req<{ items: BackupRecord[] }>('/system/backups');
export const createBackup = (note?: string) =>
  req<BackupRecord>('/system/backups', {
    method: 'POST',
    body: JSON.stringify({ note }),
  });
export const deleteBackup = (id: string) =>
  req<undefined>(`/system/backups/${id}`, { method: 'DELETE' });
export const restoreBackup = (id: string) =>
  req<BackupRecord>(`/system/backups/${id}/restore`, {
    method: 'POST',
    body: JSON.stringify({ confirm: true }),
  });
export const getBackupSettings = () =>
  req<BackupSettings>('/system/backup-settings');
export const updateBackupSettings = (s: {
  enabled: boolean;
  retentionCount: number;
  scheduleHour: number;
}) =>
  req<BackupSettings>('/system/backup-settings', {
    method: 'PUT',
    body: JSON.stringify(s),
  });

export type LDAPSettings = components['schemas']['LDAPSettings'];
export type LDAPTestResult = components['schemas']['LDAPTestResult'];
export const getLDAPSettings = () => req<LDAPSettings>('/system/ldap');
export const updateLDAPSettings = (b: LDAPSettings) =>
  req<LDAPSettings>('/system/ldap', { method: 'PUT', body: JSON.stringify(b) });
export const testLDAPLogin = (username: string, password: string) =>
  req<LDAPTestResult>('/system/ldap/test', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });

export type SecuritySettings = components['schemas']['SecuritySettings'];
export const getSecuritySettings = () =>
  req<SecuritySettings>('/system/security-settings');
export const updateSecuritySettings = (forcePasswordChange: boolean) =>
  req<SecuritySettings>('/system/security-settings', {
    method: 'PUT',
    body: JSON.stringify({ forcePasswordChange }),
  });

export type TopList = components['schemas']['TopList'];
export type TopEntry = components['schemas']['TopEntry'];
export type QpsSeries = components['schemas']['QpsSeries'];
export type QpsPoint = components['schemas']['QpsPoint'];
export type LogRow = components['schemas']['LogRow'];
export type LogPage = components['schemas']['LogPage'];
export type AuditEntry = components['schemas']['AuditEntry'];
export type AuditPage = components['schemas']['AuditPage'];

const qsOf = (o: Record<string, number | string | undefined>) => {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(o))
    if (v !== undefined && v !== '') qs.set(k, String(v));
  const s = qs.toString();
  return s ? `?${s}` : '';
};

export interface LogQuery {
  action?: string;
  answerIp?: string;
  cursor?: string;
  domain?: string;
  from: string;
  ip?: string;
  mac?: string;
  page?: number;
  pageSize?: number;
  q?: string;
  sip?: string;
  to?: string;
  type?: string;
}
export const listLogs = (q: LogQuery) =>
  req<components['schemas']['LogPage']>(`/logs${qsOf(q as never)}`);
export const listLogTop = (q: LogQuery & { by?: string; limit?: number }) =>
  req<TopList>(`/logs/top${qsOf(q as never)}`);
export const getLogQps = (q: LogQuery & { intervalSec?: number }) =>
  req<QpsSeries>(`/logs/qps${qsOf(q as never)}`);

export interface AuditQuery {
  action?: string;
  actorType?: string;
  cursor?: string;
  from: string;
  pageSize?: number;
  q?: string;
  to?: string;
}
export const listAudits = (q: AuditQuery) =>
  req<AuditPage>(`/audits${qsOf(q as never)}`);

export type AuditVerifyResult = components['schemas']['AuditVerifyResult'];
export const verifyAudits = (from: string, to?: string) => {
  const qs = new URLSearchParams({ from });
  if (to) qs.set('to', to);
  return req<AuditVerifyResult>(`/audits/verify?${qs.toString()}`);
};

// SSE live-tail（M4-003 /logs/tail）：EventSource 原生断线重连
// （同源 Cookie/Authorization 由 URL from 参数+无鉴权读端点决定，PoC 免头）
// SSE live-tail：原生 EventSource 无法携带 Authorization（RBAC 401 → 重连循环），
// 改 fetch+ReadableStream 流式解析（M2-036 修复）。
export function openLogTail(
  params: {
    action?: string;
    domain?: string;
    from?: string;
    ip?: string;
    mac?: string;
    sip?: string;
    type?: string;
  },
  handlers: {
    onError?: (e: Event) => void;
    onOpen?: () => void;
    onRow: (row: any) => void;
  },
): { close: () => void } {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) qs.set(k, String(v));
  const ctrl = new AbortController();
  void (async () => {
    try {
      const token = useAccessStore().accessToken;
      const headers: Record<string, string> = {};
      if (token) headers.Authorization = `Bearer ${token}`;
      const res = await fetch(
        `${BASE}/logs/tail${qs.size > 0 ? `?${qs}` : ''}`,
        {
          headers,
          signal: ctrl.signal,
        },
      );
      if (!res.ok || !res.body) {
        handlers.onError?.(new Event('error'));
        return;
      }
      handlers.onOpen?.();
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const frames = buf.split('\n\n');
        buf = frames.pop() ?? '';
        for (const frame of frames) {
          for (const line of frame.split('\n')) {
            if (!line.startsWith('data:')) continue;
            const payload = line.slice(5).trim();
            if (!payload) continue;
            try {
              handlers.onRow(JSON.parse(payload));
            } catch {
              /* 忽略非 JSON 心跳帧 */
            }
          }
        }
      }
      handlers.onError?.(new Event('end'));
    } catch {
      if (!ctrl.signal.aborted) handlers.onError?.(new Event('error'));
    }
  })();
  return { close: () => ctrl.abort() };
}

// ── DNS 服务（M3 交付 API 消费）──
export type Upstream = components['schemas']['Upstream'];
export type ForwardRule = components['schemas']['ForwardRule'];
export type DnsZone = components['schemas']['DnsZone'];
export type DnsRecord = components['schemas']['DnsRecord'];
export type Blocklist = components['schemas']['Blocklist'];
export type BlocklistEntryRow = components['schemas']['BlocklistEntry'];
export type BlocklistCreateRow = components['schemas']['BlocklistCreate'];
export type PolicyGroupRow = components['schemas']['PolicyGroup'];

const j = (body: unknown): RequestInit => ({
  method: 'POST',
  body: JSON.stringify(body),
});
const patch = (body: unknown): RequestInit => ({
  method: 'PATCH',
  body: JSON.stringify(body),
});
const del: RequestInit = { method: 'DELETE' };

// 上游
export const listUpstreams = () => req<{ items: Upstream[] }>('/upstreams');
export const createUpstream = (b: Partial<Upstream>) =>
  req<Upstream>('/upstreams', j(b));
export const updateUpstream = (id: string, b: Partial<Upstream>) =>
  req<Upstream>(`/upstreams/${id}`, patch(b));
export const deleteUpstream = (id: string) =>
  req<undefined>(`/upstreams/${id}`, del);

// 转发规则
export const listForwardRules = () =>
  req<{ items: ForwardRule[] }>('/forward-rules');
export const createForwardRule = (
  b: Partial<ForwardRule> & { dryRun?: boolean },
) => req<ForwardRule>('/forward-rules', j(b));
export const updateForwardRule = (
  id: string,
  b: {
    enabled?: boolean;
    note?: string;
    upstreamIds?: string[];
  },
) => req<ForwardRule>(`/forward-rules/${id}`, patch(b));
export const deleteForwardRule = (id: string) =>
  req<undefined>(`/forward-rules/${id}`, del);

// zone 与记录
export const listDnsZones = () => req<{ items: DnsZone[] }>('/dns/zones');
export const createDnsZone = (b: {
  kind: string;
  name: string;
  primary?: string;
}) => req<DnsZone>('/dns/zones', j(b));
export const updateDnsZone = (
  id: string,
  b: { enabled?: boolean; kind?: string; name?: string; primary?: string },
) => req<DnsZone>(`/dns/zones/${id}`, patch(b));
export const deleteDnsZone = (id: string) =>
  req<undefined>(`/dns/zones/${id}`, del);
export const listDnsRecords = (zoneId: string) =>
  req<{ items: DnsRecord[] }>(`/dns/zones/${zoneId}/records`);
export const createDnsRecord = (zoneId: string, b: Partial<DnsRecord>) =>
  req<DnsRecord>(`/dns/zones/${zoneId}/records`, j(b));
export const updateDnsRecord = (
  zoneId: string,
  recordId: string,
  b: Partial<DnsRecord>,
) => req<DnsRecord>(`/dns/zones/${zoneId}/records/${recordId}`, patch(b));
export const importDnsHosts = (zoneId: string, content: string) =>
  req<{ created: number; errors?: string[]; skipped: number }>(
    '/dns/hosts/import',
    { method: 'POST', body: JSON.stringify({ zoneId, content }) },
  );

export const deleteDnsRecord = (zoneId: string, recordId: string) =>
  req<undefined>(`/dns/zones/${zoneId}/records/${recordId}`, del);
export const listLinkedRecords = (zoneId: string) =>
  req<{
    items: { mac: string; name: string; rdata: string; recType: string }[];
  }>(`/dns/zones/${zoneId}/linked`);

// 封禁名单
export const listBlocklists = () =>
  req<{ items: Blocklist[] }>('/dns/blocklists');
export const syncBlocklist = (id: string) =>
  req<unknown>(`/dns/blocklists/${id}/sync`, j({}));
export const deleteBlocklist = (id: string) =>
  req<undefined>(`/dns/blocklists/${id}`, del);
export const deleteBlocklistEntry = (id: string, pattern: string) =>
  req<undefined>(
    `/dns/blocklists/${id}/entries?pattern=${encodeURIComponent(pattern)}`,
    del,
  );
export const listBlocklistEntries = (id: string) =>
  req<{ items: BlocklistEntryRow[]; total?: number }>(
    `/dns/blocklists/${id}/entries`,
  );
export const addBlocklistEntry = (
  id: string,
  body: Partial<BlocklistEntryRow>,
) => req<BlocklistEntryRow>(`/dns/blocklists/${id}/entries`, j(body));
export const createBlocklist = (body: BlocklistCreateRow) =>
  req<Blocklist>('/dns/blocklists', j(body));
export const listPolicyGroups = () =>
  req<{ items: PolicyGroupRow[] }>('/dns/policy-groups');
export const createPolicyGroup = (
  body: components['schemas']['PolicyGroupCreate'],
) => req<PolicyGroupRow>('/dns/policy-groups', j(body));
export const compilePolicyGroup = (id: string) =>
  req<components['schemas']['RpzCompileResult']>(
    `/dns/policy-groups/${id}/compile`,
    j({}),
  );

// 名单内容（M6-023）：CSV 导入导出 / 版本回滚 / 内置分类库
export const importBlocklistEntries = (
  id: string,
  content: string,
  replace = false,
) =>
  req<{ created: number; errors?: string[]; skipped: number; total: number }>(
    `/dns/blocklists/${id}/entries/import`,
    { method: 'POST', body: JSON.stringify({ content, replace }) },
  );
export const listBlocklistVersions = (id: string) =>
  req<{ items: { createdAt: string; entries: number; version: number }[] }>(
    `/dns/blocklists/${id}/versions`,
  );
export const rollbackBlocklist = (id: string, version: number) =>
  req<Blocklist>(`/dns/blocklists/${id}/versions/${version}/rollback`, j({}));

export type BuiltinPackRow = components['schemas']['BuiltinPack'];
export const listBuiltinPacks = () =>
  req<{ items: BuiltinPackRow[] }>('/dns/blocklists/builtin');
export const installBuiltinPack = (key: string) =>
  req<Blocklist>(`/dns/blocklists/builtin/${key}/install`, j({}));

export async function exportBlocklistCsv(id: string): Promise<void> {
  const token = useAccessStore().accessToken;
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${BASE}/dns/blocklists/${id}/entries/export`, {
    headers,
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { detail?: string };
    throw new Error(body.detail || `HTTP ${res.status}`);
  }
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download =
    res.headers.get('Content-Disposition')?.match(/filename="([^"]+)"/)?.[1] ??
    'blocklist.csv';
  a.click();
  URL.revokeObjectURL(a.href);
}

// ── DHCP 子网与池（M2-002 API 消费）──
// GET /orgs 返回组织树（与 /orgs/tree 同源；flat 场景前端自行展平）
export const listOrgs = () => req<OrgTreeNode[]>('/orgs');

export type SubnetOptionIn = {
  code: number;
  csvFormat?: boolean;
  data: string;
  enabled?: boolean;
  name?: string;
};
export const createSubnet = (b: {
  cidr: string;
  dnsServers?: string;
  family: 4 | 6;
  gateway?: string;
  name: string;
  options?: SubnetOptionIn[];
  orgId: string;
  owner?: string;
  pools?: {
    delegatedLen?: number;
    endAddr?: string;
    kind: string;
    prefixLen?: number;
    startAddr: string;
  }[];
  purpose?: string;
  site?: string;
  validLifetime?: number;
  vlan?: string;
}) => req<Subnet>('/subnets', j(b));
export const updateSubnet = (
  id: string,
  b: {
    cidr?: string;
    dnsServers?: string;
    gateway?: string;
    name?: string;
    options?: SubnetOptionIn[];
    owner?: string;
    pools?: {
      delegatedLen?: number;
      endAddr?: string;
      kind: string;
      prefixLen?: number;
      startAddr: string;
    }[];
    purpose?: string;
    site?: string;
    validLifetime?: number;
    vlan?: string;
  },
) => req<Subnet>(`/subnets/${id}`, patch(b));
export const deleteSubnet = (id: string) =>
  req<undefined>(`/subnets/${id}`, del);

// ── 组织管理（M2-001 API 消费，系统管理★主数据）──
export const createOrg = (b: { name: string; parentId?: null | string }) =>
  req<OrgTreeNode>('/orgs', j(b));
export const updateOrg = (
  id: string,
  b: { name?: string; parentId?: null | string },
) => req<OrgTreeNode>(`/orgs/${id}`, patch(b));
export const deleteOrg = (id: string) => req<undefined>(`/orgs/${id}`, del);

// ── DNS 缓存与性能 / 安全参数（M3-005 API 消费）──
export type DnsSettings = components['schemas']['DnsSettings'];
export type DiagnoseResult = components['schemas']['DiagnoseResult'];
export type DiagnoseRequestRow = components['schemas']['DiagnoseRequest'];
export const diagnoseDns = (b: DiagnoseRequestRow) =>
  req<DiagnoseResult>('/dns/diagnose', j(b));
export type PerDomainTtl = components['schemas']['PerDomainTtl'];

export const getDnsSettings = () => req<DnsSettings>('/dns/settings');
export const updateDnsSettings = (b: DnsSettings) =>
  req<DnsSettings>('/dns/settings', { method: 'PUT', body: JSON.stringify(b) });
export const listTtlOverrides = () =>
  req<{ items: PerDomainTtl[] }>('/dns/settings/ttl-overrides');
export const upsertTtlOverride = (b: { domain: string; ttl: number }) =>
  req<PerDomainTtl>('/dns/settings/ttl-overrides', j(b));
export const flushCache = (zone?: string) =>
  req<{ cmd: string; flushed: string }>(
    `/dns/cache/flush${zone ? `?zone=${zone}` : ''}`,
    j({}),
  );

// ── 双栈绑定模板（M2-012，§4.3 多池对）──
export type DualstackTemplate = components['schemas']['DualstackTemplate'];
export type DualstackMatchScheme =
  | 'admin'
  | 'auto'
  | 'client-id'
  | 'duid-llt'
  | 'hostname'
  | 'option79';

export const listDualstackTemplates = () =>
  req<{ items: DualstackTemplate[] }>('/dualstack/templates');
export const createDualstackTemplate = (b: {
  dnsSync?: boolean;
  encoding: string;
  expr: string;
  graceHours?: number;
  ipv4Cidr: string;
  ipv6Prefix: string;
  matchScheme?: DualstackMatchScheme;
  name: string;
}) => req<DualstackTemplate>('/dualstack/templates', j(b));
export const deleteDualstackTemplate = (id: string) =>
  req<undefined>(`/dualstack/templates/${id}`, del);
export const updateDualstackTemplate = (
  id: string,
  body: Partial<DualstackTemplate>,
) => req<DualstackTemplate>(`/dualstack/templates/${id}`, patch(body));

// ── MAC↔DUID 映射与冲突清单（M3-013，§4.5）──
export type DualstackIdentity = components['schemas']['DualstackIdentity'];
export type DualstackConflict = components['schemas']['DualstackConflict'];

export const listDualstackIdentities = () =>
  req<{ items: DualstackIdentity[] }>('/dualstack/identities');
export const createDualstackIdentity = (b: {
  duid: string;
  mac: string;
  note?: string;
}) => req<DualstackIdentity>('/dualstack/identities', j(b));
export const deleteDualstackIdentity = (mac: string) =>
  req<undefined>(`/dualstack/identities/${encodeURIComponent(mac)}`, del);
export const listDualstackConflicts = () =>
  req<{ items: DualstackConflict[] }>('/dualstack/conflicts');

// ── 用户与角色（M5-004，§13.4 系统管理）──
export type UserRow = components['schemas']['User'];
export const listUsers = () => req<{ items: UserRow[] }>('/users');
export const createUser = (b: {
  displayName?: string;
  password: string;
  roles?: string[];
  username: string;
}) => req<UserRow>('/users', j(b));
export const updateUser = (
  id: string,
  b: {
    displayName?: string;
    enabled?: boolean;
    password?: string;
    roles?: string[];
    totpReset?: boolean;
  },
) => req<UserRow>(`/users/${id}`, patch(b));
export const deleteUser = (id: string) => req<undefined>(`/users/${id}`, del);

// ── 登录会话（M6-007C）：自助列表/下线 + 管理端强制下线 ──
export type SessionRow = components['schemas']['Session'];
export const listSessions = () =>
  req<{ items: SessionRow[] }>('/auth/sessions');
export const revokeSession = (id: string) =>
  req<undefined>(`/auth/sessions/${encodeURIComponent(id)}`, del);
export const listSystemSessions = (userId?: string) =>
  req<{ items: SessionRow[] }>(
    `/system/sessions${userId ? `?userId=${encodeURIComponent(userId)}` : ''}`,
  );
export const revokeSystemSession = (id: string) =>
  req<undefined>(`/system/sessions/${encodeURIComponent(id)}`, del);

// ── DHCP 选项与类匹配（M2-016，C-02/C-03）──
export type DhcpOptionRow = components['schemas']['DhcpOption'];
export type DhcpLease6Row = components['schemas']['DhcpLease6'];
export type DhcpLease4Row = components['schemas']['DhcpLease4'];
export type DhcpLease4Update = components['schemas']['DhcpLease4Update'];
export type SubnetUtilizationPoint =
  components['schemas']['SubnetUtilizationPoint'];
export type DhcpClassRow = components['schemas']['DhcpClass'];
export type DhcpClassOptionIn = {
  data: string;
  name: string;
  optionCode: number;
};
export const listDhcpOptions = () =>
  req<{ items: DhcpOptionRow[] }>('/dhcp/options');
export const listDhcpLeases6 = () =>
  req<{ items: DhcpLease6Row[] }>('/dhcp/leases6');
export const listDhcpLeases4 = () =>
  req<{ items: DhcpLease4Row[] }>('/dhcp/leases4');
export const updateDhcpLease4 = (ip: string, b: DhcpLease4Update) =>
  req<DhcpLease4Row>(`/dhcp/leases4/${encodeURIComponent(ip)}`, patch(b));
export const deleteDhcpLease4 = (ip: string) =>
  req<undefined>(`/dhcp/leases4/${encodeURIComponent(ip)}`, del);
export const deleteDhcpLease6 = (ip: string, leaseType: 'IA_NA' | 'IA_PD') =>
  req<undefined>(
    `/dhcp/leases6/${encodeURIComponent(ip)}?leaseType=${leaseType}`,
    del,
  );
export const listSubnetUtilization = () =>
  req<{ items: SubnetUtilizationPoint[] }>('/dhcp/utilization');
export const getSubnetUtilization = (subnetId: string, hours = 24) =>
  req<{
    cidr?: string;
    points: SubnetUtilizationPoint[];
    subnetId: string;
    subnetName?: string;
  }>(`/dhcp/utilization/${subnetId}?hours=${hours}`);
export const createDhcpOption = (b: {
  data: string;
  enabled?: boolean;
  name: string;
  optionCode: number;
}) => req<DhcpOptionRow>('/dhcp/options', j(b));
export const updateDhcpOption = (
  id: string,
  b: { data?: string; enabled?: boolean; name?: string; optionCode?: number },
) => req<DhcpOptionRow>(`/dhcp/options/${id}`, patch(b));
export const deleteDhcpOption = (id: string) =>
  req<undefined>(`/dhcp/options/${id}`, del);
export const listDhcpClasses = () =>
  req<{ items: DhcpClassRow[] }>('/dhcp/classes');
export const createDhcpClass = (b: {
  enabled?: boolean;
  name: string;
  options: DhcpClassOptionIn[];
  test: string;
}) => req<DhcpClassRow>('/dhcp/classes', j(b));
export const updateDhcpClass = (
  id: string,
  b: { enabled?: boolean; options?: DhcpClassOptionIn[]; test?: string },
) => req<DhcpClassRow>(`/dhcp/classes/${id}`, patch(b));
export const deleteDhcpClass = (id: string) =>
  req<undefined>(`/dhcp/classes/${id}`, del);

// ── 保留与绑定（M2-017，§561 菜单 4/6；bulk CSV 语义事务性）──
export type ReservationBulkEntryIn = {
  address: string;
  kind: 'bind' | 'reserve';
  mac?: string;
  reason?: string;
};
export type ReservationBulkResult =
  components['schemas']['ReservationBulkResult'];
export const bulkReservations = (b: {
  entries: ReservationBulkEntryIn[];
  subnetId: string;
}) => req<ReservationBulkResult>('/reservations/bulk', j(b));

// 导出日志 CSV（M2-029）：blob 下载（非 JSON——不走 req 封装）。
export async function exportLogsCsv(
  params: Record<string, string>,
): Promise<void> {
  const token = useAccessStore().accessToken;
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const q = new URLSearchParams(params).toString();
  const res = await fetch(`${BASE}/logs/export${q ? `?${q}` : ''}`, {
    headers,
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { detail?: string };
    throw new Error(body.detail || `HTTP ${res.status}`);
  }
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download =
    res.headers.get('Content-Disposition')?.match(/filename="([^"]+)"/)?.[1] ??
    'logs.csv';
  a.click();
  URL.revokeObjectURL(a.href);
}

// ── 角色管理（M2-035，system 域）──
export type RoleRow = components['schemas']['Role'];
export const listRoles = () => req<{ items: RoleRow[] }>('/roles');
export const createRole = (b: { name: string; permissions: string[] }) =>
  req<RoleRow>('/roles', j(b));
export const updateRole = (name: string, b: { permissions: string[] }) =>
  req<RoleRow>(`/roles/${name}`, patch(b));
export const deleteRole = (name: string) =>
  req<undefined>(`/roles/${name}`, del);

// ── 日志存储策略与归档（日志生命周期管理）──
export type LogSettings = components['schemas']['LogSettings'];
export type LogArchive = components['schemas']['LogArchive'];
export type LogStorageStats = components['schemas']['LogStorageStats'];

export const getLogSettings = () => req<LogSettings>('/system/log-settings');
export const updateLogSettings = (b: {
  archiveKeepMonths?: number;
  autoExport?: boolean;
  retentionDays?: number;
}) =>
  req<LogSettings>('/system/log-settings', {
    method: 'PUT',
    body: JSON.stringify(b),
  });
export const getLogStorageStats = () =>
  req<LogStorageStats>('/system/log-archives/stats');
export const listLogArchives = () =>
  req<{ items: LogArchive[] }>('/system/log-archives');
export const exportLogArchive = (month: string) =>
  req<LogArchive>(`/system/log-archives/${month}/export`, { method: 'POST' });
export const deleteLogArchive = (month: string) =>
  req<undefined>(`/system/log-archives/${month}`, { method: 'DELETE' });

// Parquet 归档下载（非 JSON——不走 req 封装）
export async function downloadLogArchive(month: string): Promise<void> {
  const token = useAccessStore().accessToken;
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${BASE}/system/log-archives/${month}/download`, {
    headers,
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { detail?: string };
    throw new Error(body.detail || `HTTP ${res.status}`);
  }
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `logs-${month}.parquet`;
  a.click();
  URL.revokeObjectURL(a.href);
}
