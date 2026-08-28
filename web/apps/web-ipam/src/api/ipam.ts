import type { components } from './schema';

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
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error((body as { code?: string; detail?: string }).detail || `HTTP ${res.status}`);
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

export const listOrgTree = () => req<OrgTreeNode[]>('/orgs/tree');

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
  from: string; to?: string; type?: string; mac?: string; ip?: string;
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
export function openLogTail(
  params: { from?: string; type?: string; domain?: string; action?: string },
  handlers: { onRow: (row: any) => void; onOpen?: () => void; onError?: (e: Event) => void },
): EventSource {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) qs.set(k, String(v));
  const es = new EventSource(`${BASE}/logs/tail${qs.size ? `?${qs}` : ''}`);
  es.onopen = () => handlers.onOpen?.();
  es.onerror = (e) => handlers.onError?.(e);
  es.addEventListener('log', (ev) => {
    try {
      handlers.onRow(JSON.parse((ev as MessageEvent).data));
    } catch {
      /* 忽略非 JSON 心跳帧 */
    }
  });
  return es;
}

// ── DNS 服务（M3 交付 API 消费）──
export type Upstream = components['schemas']['Upstream'];
export type ForwardRule = components['schemas']['ForwardRule'];
export type DnsZone = components['schemas']['DnsZone'];
export type DnsRecord = components['schemas']['DnsRecord'];
export type Blocklist = components['schemas']['Blocklist'];

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
export const deleteForwardRule = (id: string) => req<void>(`/forward-rules/${id}`, del);

// zone 与记录
export const listDnsZones = () => req<{ items: DnsZone[] }>('/dns/zones');
export const createDnsZone = (b: { name: string; kind: string }) => req<DnsZone>('/dns/zones', j(b));
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

// ── DHCP 子网与池（M2-002 API 消费）──
// GET /orgs 返回组织树（与 /orgs/tree 同源；flat 场景前端自行展平）
export const listOrgs = () => req<OrgTreeNode[]>('/orgs');

export const createSubnet = (b: {
  orgId: string; name: string; family: 4 | 6; cidr: string;
  pools?: { startAddr: string; endAddr: string; kind: string }[];
}) => req<Subnet>('/subnets', j(b));
export const deleteSubnet = (id: string) => req<void>(`/subnets/${id}`, del);
