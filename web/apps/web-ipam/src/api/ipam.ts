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
export type Asset = components['schemas']['Asset'];

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
      (body as { code?: string; detail?: string }).detail || `HTTP ${res.status}`,
    );
  }
  return res.json() as Promise<T>;
}

export const listLedger = (params: { orgId?: string; family?: number; state?: string; cursor?: string; pageSize?: number }) => {
  const qs = new URLSearchParams();
  if (params.orgId) qs.set('orgId', params.orgId);
  if (params.family) qs.set('family', String(params.family));
  if (params.state) qs.set('state', params.state);
  if (params.cursor) qs.set('cursor', params.cursor);
  if (params.pageSize) qs.set('pageSize', String(params.pageSize));
  const q = qs.toString();
  return req<LedgerPage>(`/ledger${q ? `?${q}` : ''}`);
};

export const listOrgTree = () => req<OrgTreeNode[]>('/orgs');

export const reserveAddress = (subnetId: string, address: string) =>
  req<void>('/ledger', { method: 'POST', body: JSON.stringify({ subnetId, address }) });

export const bindStatic = (subnetId: string, address: string, mac: string) =>
  req<void>('/ledger/bind', { method: 'POST', body: JSON.stringify({ subnetId, address, mac }) });

export const listSubnets = (orgId?: string) =>
  req<{ items: Subnet[] }>(`/subnets${orgId ? `?orgId=${orgId}` : ''}`);

export const listAssets = (orgId?: string) =>
  req<{ items: Asset[] }>(`/assets${orgId ? `?orgId=${orgId}` : ''}`);
export type DashboardOverview = components['schemas']['DashboardOverview'];
export type PoolUtilization = components['schemas']['PoolUtilization'];

export const getDashboard = (poolTopN?: number) =>
  req<DashboardOverview>(`/dashboard${poolTopN ? `?poolTopN=${poolTopN}` : ''}`);

export type TopList = components['schemas']['TopList'];
export type QpsSeries = components['schemas']['QpsSeries'];
export type AuditEntry = components['schemas']['AuditEntry'];
export type AuditPage = components['schemas']['AuditPage'];

const qsOf = (o: Record<string, string | number | undefined>) => {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== '') qs.set(k, String(v));
  const s = qs.toString();
  return s ? `?${s}` : '';
};

export interface LogQuery {
  from: string; to?: string; type?: string; mac?: string; ip?: string; answerIp?: string;
  domain?: string; action?: string; cursor?: string; pageSize?: number;
}
export const listLogs = (q: LogQuery) =>
  req<components['schemas']['LogPage']>(`/logs${qsOf(q as never)}`);
export const listLogTop = (q: LogQuery & { by?: string; limit?: number }) =>
  req<TopList>(`/logs/top${qsOf(q as never)}`);
export const getLogQps = (q: LogQuery & { intervalSec?: number }) =>
  req<QpsSeries>(`/logs/qps${qsOf(q as never)}`);

export interface AuditQuery {
  from: string; to?: string; actorType?: string; action?: string;
  q?: string; cursor?: string; pageSize?: number;
}
export const listAudits = (q: AuditQuery) =>
  req<AuditPage>(`/audits${qsOf(q as never)}`);

// SSE live-tail（M4-003 /logs/tail）：EventSource 原生断线重连
// （同源 Cookie/Authorization 由 URL from 参数+无鉴权读端点决定，PoC 免头）
// SSE live-tail：原生 EventSource 无法携带 Authorization（RBAC 401 → 重连循环），
// 改 fetch+ReadableStream 流式解析（M2-036 修复）。
export function openLogTail(
  params: { from?: string; type?: string; domain?: string; action?: string },
  handlers: { onRow: (row: any) => void; onOpen?: () => void; onError?: (e: Event) => void },
): { close: () => void } {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) qs.set(k, String(v));
  const ctrl = new AbortController();
  void (async () => {
    try {
      const token = useAccessStore().accessToken;
      const headers: Record<string, string> = {};
      if (token) headers.Authorization = `Bearer ${token}`;
      const res = await fetch(`${BASE}/logs/tail${qs.size ? `?${qs}` : ''}`, {
        headers, signal: ctrl.signal,
      });
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

const j = (body: unknown): RequestInit => ({ method: 'POST', body: JSON.stringify(body) });
const patch = (body: unknown): RequestInit => ({ method: 'PATCH', body: JSON.stringify(body) });
const del: RequestInit = { method: 'DELETE' };

// 上游
export const listUpstreams = () => req<{ items: Upstream[] }>('/upstreams');
export const createUpstream = (b: Partial<Upstream>) => req<Upstream>('/upstreams', j(b));
export const updateUpstream = (id: string, b: Partial<Upstream>) => req<Upstream>(`/upstreams/${id}`, patch(b));
export const deleteUpstream = (id: string) => req<void>(`/upstreams/${id}`, del);

// 转发规则
export const listForwardRules = () => req<{ items: ForwardRule[] }>('/forward-rules');
export const createForwardRule = (b: Partial<ForwardRule> & { dryRun?: boolean }) =>
  req<ForwardRule>('/forward-rules', j(b));
export const updateForwardRule = (id: string, b: {
  upstreamIds?: string[]; enabled?: boolean; note?: string;
}) => req<ForwardRule>(`/forward-rules/${id}`, patch(b));
export const deleteForwardRule = (id: string) => req<void>(`/forward-rules/${id}`, del);

// zone 与记录
export const listDnsZones = () => req<{ items: DnsZone[] }>('/dns/zones');
export const createDnsZone = (b: { name: string; kind: string }) => req<DnsZone>('/dns/zones', j(b));
export const deleteDnsZone = (id: string) => req<void>(`/dns/zones/${id}`, del);
export const listDnsRecords = (zoneId: string) =>
  req<{ items: DnsRecord[] }>(`/dns/zones/${zoneId}/records`);
export const createDnsRecord = (zoneId: string, b: Partial<DnsRecord>) =>
  req<DnsRecord>(`/dns/zones/${zoneId}/records`, j(b));
export const deleteDnsRecord = (zoneId: string, recordId: string) =>
  req<void>(`/dns/zones/${zoneId}/records/${recordId}`, del);
export const listLinkedRecords = (zoneId: string) =>
  req<{ items: { name: string; recType: string; rdata: string; mac: string }[] }>(
    `/dns/zones/${zoneId}/linked`);

// 封禁名单
export const listBlocklists = () => req<{ items: Blocklist[] }>('/dns/blocklists');
export const syncBlocklist = (id: string) => req<unknown>(`/dns/blocklists/${id}/sync`, j({}));
export const deleteBlocklist = (id: string) => req<void>(`/dns/blocklists/${id}`, del);
export const deleteBlocklistEntry = (id: string, pattern: string) => req<void>(`/dns/blocklists/${id}/entries?pattern=${encodeURIComponent(pattern)}`, del);
export const listBlocklistEntries = (id: string) => req<{ items: BlocklistEntryRow[]; total?: number }>(`/dns/blocklists/${id}/entries`);
export const addBlocklistEntry = (id: string, body: Partial<BlocklistEntryRow>) => req<BlocklistEntryRow>(`/dns/blocklists/${id}/entries`, j(body));
export const createBlocklist = (body: BlocklistCreateRow) => req<Blocklist>('/dns/blocklists', j(body));
export const listPolicyGroups = () => req<{ items: PolicyGroupRow[] }>('/dns/policy-groups');
export const createPolicyGroup = (body: components['schemas']['PolicyGroupCreate']) => req<PolicyGroupRow>('/dns/policy-groups', j(body));
export const compilePolicyGroup = (id: string) => req<components['schemas']['RpzCompileResult']>(`/dns/policy-groups/${id}/compile`, j({}));

// ── DHCP 子网与池（M2-002 API 消费）──
// GET /orgs 返回组织树（与 /orgs/tree 同源；flat 场景前端自行展平）
export const listOrgs = () => req<OrgTreeNode[]>('/orgs');

export const createSubnet = (b: {
  orgId: string; name: string; family: 4 | 6; cidr: string; gateway?: string; dnsServers?: string;
  pools?: { startAddr: string; endAddr?: string; kind: string; prefixLen?: number; delegatedLen?: number }[];
}) => req<Subnet>('/subnets', j(b));
export const updateSubnet = (id: string, b: {
  name?: string; cidr?: string; gateway?: string; dnsServers?: string;
  pools?: { startAddr: string; endAddr?: string; kind: string; prefixLen?: number; delegatedLen?: number }[];
}) => req<Subnet>(`/subnets/${id}`, patch(b));
export const deleteSubnet = (id: string) => req<void>(`/subnets/${id}`, del);

// ── 组织管理（M2-001 API 消费，系统管理★主数据）──
export const createOrg = (b: { parentId?: string | null; name: string }) =>
  req<OrgTreeNode>('/orgs', j(b));
export const updateOrg = (
  id: string,
  b: { parentId?: string | null; name?: string },
) => req<OrgTreeNode>(`/orgs/${id}`, patch(b));
export const deleteOrg = (id: string) => req<void>(`/orgs/${id}`, del);

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
  req<{ flushed: string; cmd: string }>(`/dns/cache/flush${zone ? `?zone=${zone}` : ''}`, j({}));

// ── 双栈绑定模板（M2-012，§4.3 多池对）──
export type DualstackTemplate = components['schemas']['DualstackTemplate'];

export const listDualstackTemplates = () =>
  req<{ items: DualstackTemplate[] }>('/dualstack/templates');
export const createDualstackTemplate = (b: {
  name: string; ipv4Cidr: string; ipv6Prefix: string;
  encoding: string; expr: string; dnsSync?: boolean; graceHours?: number;
}) => req<DualstackTemplate>('/dualstack/templates', j(b));
export const deleteDualstackTemplate = (id: string) =>
  req<void>(`/dualstack/templates/${id}`, del);
export const updateDualstackTemplate = (id: string, body: Partial<DualstackTemplate>) => req<DualstackTemplate>(`/dualstack/templates/${id}`, j(body));

// ── 用户与角色（M5-004，§13.4 系统管理）──
export type UserRow = components['schemas']['User'];
export const listUsers = () => req<{ items: UserRow[] }>('/users');
export const createUser = (b: {
  displayName?: string; password: string; roles?: string[]; username: string;
}) => req<UserRow>('/users', j(b));
export const updateUser = (id: string, b: {
  displayName?: string; enabled?: boolean; password?: string; roles?: string[];
}) => req<UserRow>(`/users/${id}`, patch(b));
export const deleteUser = (id: string) => req<void>(`/users/${id}`, del);

// ── DHCP 选项与类匹配（M2-016，C-02/C-03）──
export type DhcpOptionRow = components['schemas']['DhcpOption'];
export type DhcpLease6Row = components['schemas']['DhcpLease6'];
export type DhcpClassRow = components['schemas']['DhcpClass'];
export type DhcpClassOptionIn = { optionCode: number; name: string; data: string };
export const listDhcpOptions = () => req<{ items: DhcpOptionRow[] }>('/dhcp/options');
export const listDhcpLeases6 = () => req<{ items: DhcpLease6Row[] }>('/dhcp/leases6');
export const createDhcpOption = (b: { optionCode: number; name: string; data: string; enabled?: boolean }) =>
  req<DhcpOptionRow>('/dhcp/options', j(b));
export const updateDhcpOption = (id: string, b: { optionCode?: number; name?: string; data?: string; enabled?: boolean }) =>
  req<DhcpOptionRow>(`/dhcp/options/${id}`, patch(b));
export const deleteDhcpOption = (id: string) => req<void>(`/dhcp/options/${id}`, del);
export const listDhcpClasses = () => req<{ items: DhcpClassRow[] }>('/dhcp/classes');
export const createDhcpClass = (b: { name: string; test: string; options: DhcpClassOptionIn[]; enabled?: boolean }) =>
  req<DhcpClassRow>('/dhcp/classes', j(b));
export const updateDhcpClass = (id: string, b: { test?: string; options?: DhcpClassOptionIn[]; enabled?: boolean }) =>
  req<DhcpClassRow>(`/dhcp/classes/${id}`, patch(b));
export const deleteDhcpClass = (id: string) => req<void>(`/dhcp/classes/${id}`, del);

// ── 保留与绑定（M2-017，§561 菜单 4/6；bulk CSV 语义事务性）──
export type ReservationBulkEntryIn = {
  kind: 'bind' | 'reserve';
  address: string;
  mac?: string;
  reason?: string;
};
export type ReservationBulkResult = components['schemas']['ReservationBulkResult'];
export const bulkReservations = (b: {
  entries: ReservationBulkEntryIn[];
  subnetId: string;
}) => req<ReservationBulkResult>('/reservations/bulk', j(b));

// 导出日志 CSV（M2-029）：blob 下载（非 JSON——不走 req 封装）。
export async function exportLogsCsv(params: Record<string, string>): Promise<void> {
  const token = useAccessStore().accessToken;
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const q = new URLSearchParams(params).toString();
  const res = await fetch(`${BASE}/logs/export${q ? `?${q}` : ''}`, { headers });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { detail?: string };
    throw new Error(body.detail || `HTTP ${res.status}`);
  }
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = res.headers.get('Content-Disposition')?.match(/filename="([^"]+)"/)?.[1] ?? 'logs.csv';
  a.click();
  URL.revokeObjectURL(a.href);
}

// ── 角色管理（M2-035，system 域）──
export type RoleRow = components['schemas']['Role'];
export const listRoles = () => req<{ items: RoleRow[] }>('/roles');
export const createRole = (b: { name: string; permissions: string[] }) => req<RoleRow>('/roles', j(b));
export const updateRole = (name: string, b: { permissions: string[] }) => req<RoleRow>(`/roles/${name}`, j(b));
export const deleteRole = (name: string) => req<void>(`/roles/${name}`, del);
