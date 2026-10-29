<script setup lang="ts">
import type { VxeGridProps } from '@vben/plugins/vxe-table';

import type {
  DhcpLease6Row,
  OrgTreeNode,
  Subnet,
  SubnetOptionIn,
} from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { useVbenDrawer, useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Divider,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Switch,
  Tag,
  Tree,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  createSubnet,
  deleteSubnet,
  listDhcpLeases6,
  listLedger,
  listOrgTree,
  listSubnets,
  updateSubnet,
} from '#/api/ipam';
import OrgManageCard from '#/components/org-manage-card.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

import { DHCP4_OPTION_PRESETS, DHCP6_OPTION_PRESETS } from './options';

type Family = 4 | 6;
type FamilyTab = 'v4' | 'v6';
type Pool = Subnet['pools'][number];

interface OrgStat {
  online: number;
  subnets: number;
  v4: number;
  v6: number;
}

interface OrgAntNode {
  children?: OrgAntNode[];
  isLeaf?: boolean;
  key: string;
  org: OrgTreeNode;
  title: string;
}

const EMPTY_STAT: OrgStat = { online: 0, subnets: 0, v4: 0, v6: 0 };

// IPv6 容量可达 2^128，必须用 BigInt 精确计算（见 vite.config.ts 的 es2020 目标）
const B0 = 0n;
const B1 = 1n;
const B16 = 16n;
const B10K = 10_000n;
const B1M = 1_000_000n;
const B1B = 1_000_000_000n;

const route = useRoute();
const router = useRouter();

// ── 页面数据 ───────────────────────────────────────────────────────────
const allRows = ref<Subnet[]>([]);
const orgTree = ref<OrgTreeNode[]>([]);
const onlineBySubnet = ref<Record<string, number>>({});
const onlineTotals = ref({ v4: 0, v6: 0 });
const loading = ref(false);

const familyTab = ref<FamilyTab>('v4');
const filterOrgId = ref('');
const orgSearch = ref('');
const orgExpandedKeys = ref<string[]>([]);

// ── 组织树：搜索 / 统计 / 展开 ─────────────────────────────────────────
function toAntNodes(nodes: OrgTreeNode[], keyword: string): OrgAntNode[] {
  const out: OrgAntNode[] = [];
  for (const n of nodes) {
    const children = toAntNodes(n.children ?? [], keyword);
    const hit = !keyword || n.name.toLowerCase().includes(keyword);
    if (!hit && children.length === 0) continue;
    out.push({
      key: n.id,
      title: n.name,
      org: n,
      isLeaf: !n.children?.length,
      children: children.length > 0 ? children : undefined,
    });
  }
  return out;
}

const orgTreeData = computed(() =>
  toAntNodes(orgTree.value, orgSearch.value.trim().toLowerCase()),
);

const effectiveExpandedKeys = computed(() =>
  orgSearch.value.trim() ? collectAntKeys(orgTreeData.value) : orgExpandedKeys.value,
);

function collectAntKeys(nodes: OrgAntNode[]): string[] {
  const out: string[] = [];
  const walk = (list: OrgAntNode[]) => {
    for (const n of list) {
      out.push(n.key);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

function collectOrgKeys(nodes: OrgTreeNode[]): string[] {
  const out: string[] = [];
  const walk = (list: OrgTreeNode[]) => {
    for (const n of list) {
      out.push(n.id);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

function collectSubtreeIds(nodes: OrgTreeNode[], target: string): null | Set<string> {
  for (const n of nodes) {
    if (n.id === target) {
      const ids = new Set<string>();
      const walk = (list: OrgTreeNode[]) => {
        for (const item of list) {
          ids.add(item.id);
          if (item.children?.length) walk(item.children);
        }
      };
      walk([n]);
      return ids;
    }
    const hit = collectSubtreeIds(n.children ?? [], target);
    if (hit) return hit;
  }
  return null;
}

const orgPathMap = computed<Record<string, string[]>>(() => {
  const out: Record<string, string[]> = {};
  const walk = (nodes: OrgTreeNode[], trail: string[]) => {
    for (const n of nodes) {
      const next = [...trail, n.name];
      out[n.id] = next;
      walk(n.children ?? [], next);
    }
  };
  walk(orgTree.value, []);
  return out;
});

function orgPathOf(orgId?: string): string[] {
  if (!orgId) return [];
  return orgPathMap.value[orgId] ?? [];
}

const orgScopeLabel = computed(() =>
  filterOrgId.value
    ? orgPathOf(filterOrgId.value).join(' / ') || '全部组织'
    : '全部组织',
);

// 组织统计：本节点 + 全部下级节点
const orgStats = computed<Record<string, OrgStat>>(() => {
  const direct: Record<string, OrgStat> = {};
  for (const r of allRows.value) {
    const stat = (direct[r.orgId] ??= { online: 0, subnets: 0, v4: 0, v6: 0 });
    stat.subnets += 1;
    stat.online += onlineBySubnet.value[r.id] ?? 0;
    if (r.family === 6) stat.v6 += 1;
    else stat.v4 += 1;
  }
  const out: Record<string, OrgStat> = {};
  const walk = (nodes: OrgTreeNode[]): OrgStat => {
    const acc: OrgStat = { online: 0, subnets: 0, v4: 0, v6: 0 };
    for (const n of nodes) {
      const self = direct[n.id];
      const child = walk(n.children ?? []);
      const stat: OrgStat = {
        subnets: (self?.subnets ?? 0) + child.subnets,
        online: (self?.online ?? 0) + child.online,
        v4: (self?.v4 ?? 0) + child.v4,
        v6: (self?.v6 ?? 0) + child.v6,
      };
      out[n.id] = stat;
      acc.subnets += stat.subnets;
      acc.online += stat.online;
      acc.v4 += stat.v4;
      acc.v6 += stat.v6;
    }
    return acc;
  };
  walk(orgTree.value);
  return out;
});

function statsOf(orgId: string): OrgStat {
  return orgStats.value[orgId] ?? EMPTY_STAT;
}

function onOrgSelect(_keys: unknown, info: unknown) {
  const node = (info as { node?: { org?: OrgTreeNode } }).node;
  const id = node?.org?.id;
  if (!id) return;
  filterOrgId.value = id === filterOrgId.value ? '' : id;
  syncQuery();
}

function onOrgExpand(keys: unknown) {
  orgExpandedKeys.value = Array.isArray(keys) ? keys.map(String) : [];
}

function selectOrg(orgId: string) {
  filterOrgId.value = orgId;
  syncQuery();
}

// ── 族切换与筛选 ───────────────────────────────────────────────────────
const scopedRows = computed(() => {
  if (!filterOrgId.value) return allRows.value;
  const ids = collectSubtreeIds(orgTree.value, filterOrgId.value);
  if (!ids) return allRows.value;
  return allRows.value.filter((r) => ids.has(r.orgId));
});

const v4Rows = computed(() =>
  sortRows(scopedRows.value.filter((r) => r.family === 4)),
);
const v6Rows = computed(() =>
  sortRows(scopedRows.value.filter((r) => r.family === 6)),
);
const visibleRows = computed(() =>
  familyTab.value === 'v6' ? v6Rows.value : v4Rows.value,
);

function sortRows(list: Subnet[]): Subnet[] {
  return list.toSorted(
    (a, b) => (a.keaSubnetId ?? 9999) - (b.keaSubnetId ?? 9999),
  );
}

function switchFamily(key: FamilyTab) {
  if (familyTab.value === key) return;
  familyTab.value = key;
  syncQuery();
}

function syncQuery() {
  const query = { ...route.query };
  if (familyTab.value === 'v6') query.family = '6';
  else delete query.family;
  if (filterOrgId.value) query.org = filterOrgId.value;
  else delete query.org;
  void router.replace({ query });
}

// ── 汇总指标 ───────────────────────────────────────────────────────────
const v4Count = computed(() => allRows.value.filter((r) => r.family === 4).length);
const v6Count = computed(() => allRows.value.filter((r) => r.family === 6).length);
const issuedCount = computed(
  () => allRows.value.filter((r) => r.keaSubnetId !== null && r.keaSubnetId !== undefined).length,
);
const pendingKea = computed(() => allRows.value.length - issuedCount.value);
const keaRatio = computed(() =>
  allRows.value.length === 0 ? 0 : (issuedCount.value / allRows.value.length) * 100,
);
const totalOnline = computed(() => onlineTotals.value.v4 + onlineTotals.value.v6);
const v4PoolCount = computed(() =>
  allRows.value.reduce(
    (n, r) => n + (r.pools ?? []).filter((p) => p.kind !== 'pd').length,
    0,
  ),
);
const v6PdCount = computed(() =>
  allRows.value.reduce(
    (n, r) => n + (r.pools ?? []).filter((p) => p.kind === 'pd').length,
    0,
  ),
);

const familyCards = computed(() =>
  (['v4', 'v6'] as const).map((key) => {
    const list = key === 'v4' ? v4Rows.value : v6Rows.value;
    return {
      key,
      title: key === 'v4' ? 'IPv4' : 'IPv6',
      tag: key === 'v4' ? '逐地址规划' : '前缀 / PD 委派',
      icon: key === 'v4' ? 'lucide:network' : 'lucide:globe',
      subnets: list.length,
      online: list.reduce((n, r) => n + (onlineBySubnet.value[r.id] ?? 0), 0),
      issued: list.filter(
        (r) => r.keaSubnetId !== null && r.keaSubnetId !== undefined,
      ).length,
    };
  }),
);

const summaryItems = computed(() => {
  const list = visibleRows.value;
  const online = list.reduce((n, r) => n + (onlineBySubnet.value[r.id] ?? 0), 0);
  const issued = list.filter(
    (r) => r.keaSubnetId !== null && r.keaSubnetId !== undefined,
  ).length;
  const dynamicPools = list.reduce(
    (n, r) => n + (r.pools ?? []).filter((p) => p.kind !== 'pd').length,
    0,
  );
  const pdPools = list.reduce(
    (n, r) => n + (r.pools ?? []).filter((p) => p.kind === 'pd').length,
    0,
  );
  const capacity = list.reduce<bigint>(
    (acc, r) => acc + (subnetCapacity(r) ?? B0),
    B0,
  );
  const items: { label: string; unit: string; value: string }[] = [
    { label: '子网', value: String(list.length), unit: '个' },
  ];
  if (familyTab.value === 'v6') {
    items.push(
      { label: 'PD 委派池', value: String(pdPools), unit: '个' },
      { label: '动态地址池', value: String(dynamicPools), unit: '个' },
      {
        label: '可分配容量',
        value: fmtBig(capacity),
        unit: capacityUnit(list),
      },
    );
  } else {
    items.push(
      { label: '动态地址池', value: String(dynamicPools), unit: '个' },
      { label: '地址容量', value: fmtBig(capacity), unit: '个地址' },
      {
        label: '平均利用率',
        value: capacity > B0 ? utilizationText(online, capacity) : '—',
        unit: capacity > B0 ? '%' : '',
      },
    );
  }
  items.push(
    { label: '在线租约', value: String(online), unit: '条' },
    { label: 'Kea 已下发', value: `${issued}/${list.length}`, unit: '' },
  );
  return items;
});

function capacityUnit(list: Subnet[]): string {
  if (familyTab.value !== 'v6') return '个地址';
  const hasPd = list.some((r) => (r.pools ?? []).some((p) => p.kind === 'pd'));
  const hasDynamic = list.some((r) =>
    (r.pools ?? []).some((p) => p.kind !== 'pd'),
  );
  if (hasPd && hasDynamic) return '个';
  return hasPd ? '个前缀' : '个地址';
}

function utilizationText(used: number, capacity: bigint): string {
  if (capacity <= B0) return '—';
  const pct = Number((BigInt(used) * B10K) / capacity) / 100;
  if (pct > 0 && pct < 0.1) return '<0.1';
  return pct.toFixed(1);
}

// ── 容量 / 利用率 ──────────────────────────────────────────────────────
function ipv4ToInt(ip: string): null | number {
  const parts = ip.trim().split('.');
  if (parts.length !== 4) return null;
  let out = 0;
  for (const part of parts) {
    const n = Number(part);
    if (!Number.isInteger(n) || n < 0 || n > 255) return null;
    out = out * 256 + n;
  }
  return out;
}

function ipv6ToBigInt(ip: string): bigint | null {
  const clean = ip.split('%')[0] ?? '';
  if (!clean.includes(':')) return null;
  const [head = '', tail] = clean.split('::');
  const parse = (segment: string): bigint[] => {
    if (!segment) return [];
    return segment.split(':').map((group) => {
      const value = Number.parseInt(group || '0', 16);
      return Number.isNaN(value) ? -B1 : BigInt(value);
    });
  };
  const left = parse(head);
  const right = tail === undefined ? [] : parse(tail);
  const missing = 8 - left.length - right.length;
  if (tail === undefined && missing !== 0) return null;
  if (missing < 0) return null;
  const groups = [
    ...left,
    ...Array.from({ length: missing }, () => B0),
    ...right,
  ];
  if (groups.some((g) => g < B0)) return null;
  return groups.reduce((acc, g) => (acc << B16) + g, B0);
}

function capacityOfPool(pool: Pool, family: number): bigint | null {
  if (pool.kind === 'pd') {
    const span = (pool.delegatedLen ?? 0) - (pool.prefixLen ?? 0);
    return span > 0 && span <= 128 ? B1 << BigInt(span) : null;
  }
  if (family === 4) {
    const start = ipv4ToInt(pool.startAddr);
    const end = ipv4ToInt(pool.endAddr);
    if (start === null || end === null || end < start) return null;
    return BigInt(end - start + 1);
  }
  const start = ipv6ToBigInt(pool.startAddr);
  const end = ipv6ToBigInt(pool.endAddr);
  if (start === null || end === null || end < start) return null;
  return end - start + B1;
}

function subnetCapacity(r: Subnet): bigint | null {
  let total: bigint | null = null;
  for (const pool of r.pools ?? []) {
    const cap = capacityOfPool(pool, r.family);
    if (cap === null) continue;
    total = (total ?? B0) + cap;
  }
  return total;
}

function isPdOnly(r: Subnet): boolean {
  const pools = r.pools ?? [];
  return pools.length > 0 && pools.every((p) => p.kind === 'pd');
}

function utilizationOf(r: Subnet): null | number {
  if (r.family === 6 && isPdOnly(r)) return null;
  const capacity = subnetCapacity(r);
  if (!capacity || capacity <= B0) return null;
  const used = onlineBySubnet.value[r.id] ?? 0;
  const pct = Number((BigInt(used) * B10K) / capacity) / 100;
  return Math.min(100, pct);
}

function toneOfPct(pct: null | number): 'amber' | 'green' | 'red' {
  if (pct === null) return 'green';
  if (pct > 90) return 'red';
  if (pct > 75) return 'amber';
  return 'green';
}

function fmtBig(value: bigint | null): string {
  if (value === null) return '—';
  if (value >= B1B) return `${trimNum(Number(value) / 1e9)}B`;
  if (value >= B1M) return `${trimNum(Number(value) / 1e6)}M`;
  if (value >= B10K) return `${trimNum(Number(value) / 1e3)}K`;
  return value.toLocaleString('zh-CN');
}

function trimNum(n: number): string {
  if (n >= 100) return n.toFixed(0);
  if (n >= 10) return n.toFixed(1);
  return n.toFixed(2);
}

function fmtLifetime(sec?: number): string {
  if (!sec) return '—';
  if (sec % 86_400 === 0) return `${sec / 86_400} 天`;
  if (sec % 3600 === 0) return `${sec / 3600} 小时`;
  if (sec % 60 === 0) return `${sec / 60} 分钟`;
  return `${sec} 秒`;
}

// ── 地址池展示 ─────────────────────────────────────────────────────────
const POOL_KIND: Record<string, { label: string; tone: string }> = {
  dynamic: { label: '动态池', tone: 'blue' },
  excluded: { label: '保留段', tone: 'muted' },
  pd: { label: 'PD 委派', tone: 'violet' },
};

function poolKinds(r: Subnet): { label: string; tone: string }[] {
  const seen = new Set<string>();
  const out: { label: string; tone: string }[] = [];
  for (const pool of r.pools ?? []) {
    if (seen.has(pool.kind)) continue;
    seen.add(pool.kind);
    out.push(POOL_KIND[pool.kind] ?? { label: pool.kind, tone: 'muted' });
  }
  return out;
}

function poolSummary(r: Subnet): string {
  const pools = r.pools ?? [];
  const first = pools[0];
  if (!first) return '';
  const one =
    first.kind === 'pd'
      ? `${first.startAddr}/${first.prefixLen ?? '?'} → /${first.delegatedLen ?? '?'}`
      : `${first.startAddr} – ${first.endAddr}`;
  return pools.length > 1 ? `${one} · 另 ${pools.length - 1} 个` : one;
}

function subnetOptions(r: Subnet): SubnetOptionIn[] {
  const raw = (r as unknown as { options?: SubnetOptionIn[] }).options;
  return Array.isArray(raw) ? raw : [];
}

// ── 数据加载 ───────────────────────────────────────────────────────────
async function fetchOnlineFamily(family: Family) {
  const counts: Record<string, number> = {};
  let cursor: string | undefined;
  let total = 0;
  for (let page = 0; page < 20; page += 1) {
    const res = await listLedger({
      family,
      state: 'online',
      cursor,
      pageSize: 500,
    });
    for (const row of res.items ?? []) {
      if (row.subnetId) counts[row.subnetId] = (counts[row.subnetId] ?? 0) + 1;
    }
    total = res.total ?? total;
    if (!res.nextCursor) break;
    cursor = res.nextCursor;
  }
  return { counts, total };
}

async function loadOnlineCounts() {
  try {
    const [v4, v6] = await Promise.all([
      fetchOnlineFamily(4),
      fetchOnlineFamily(6),
    ]);
    onlineBySubnet.value = { ...v4.counts, ...v6.counts };
    onlineTotals.value = { v4: v4.total, v6: v6.total };
  } catch {
    onlineBySubnet.value = {};
    onlineTotals.value = { v4: 0, v6: 0 };
  }
}

async function loadAll() {
  loading.value = true;
  subnetGridApi.setLoading(true);
  try {
    const [subnets, orgs] = await Promise.all([listSubnets(), listOrgTree()]);
    allRows.value = subnets.items ?? [];
    orgTree.value = orgs;
    if (
      filterOrgId.value &&
      !collectOrgKeys(orgTree.value).includes(filterOrgId.value)
    ) {
      filterOrgId.value = '';
    }
    orgExpandedKeys.value = collectOrgKeys(orgTree.value);
    await loadOnlineCounts();
  } finally {
    loading.value = false;
    subnetGridApi.setLoading(false);
  }
}

// ── 新建 / 编辑表单 ────────────────────────────────────────────────────
const editingId = ref<string>();
const [FormModal, formModalApi] = useVbenModal({
  draggable: true,
  title: '子网信息',
  confirmText: '创建（下发 Kea）',
  onConfirm: () => add(),
});

function emptyForm(family: Family, orgId = '') {
  return {
    orgId,
    name: '',
    family,
    cidr: '',
    gateway: '',
    dnsServers: '',
    poolStart: '',
    poolEnd: '',
    poolKind: 'dynamic' as 'dynamic' | 'pd',
    poolPrefixLen: 64,
    poolDelegatedLen: 80,
    validLifetime: 3600,
    options: [] as SubnetOptionIn[],
  };
}

const form = ref(emptyForm(4));
const isV6 = computed(() => form.value.family === 6);

const orgOptions = computed(() => {
  const out: { label: string; value: string }[] = [];
  const walk = (nodes: OrgTreeNode[], trail: string[]) => {
    for (const n of nodes) {
      const next = [...trail, n.name];
      out.push({ label: next.join(' / '), value: n.id });
      walk(n.children ?? [], next);
    }
  };
  walk(orgTree.value, []);
  return out;
});

function openAdd(family: Family) {
  editingId.value = undefined;
  form.value = emptyForm(family, filterOrgId.value || form.value.orgId);
  formModalApi.setState({
    title: `新建 IPv${family} 子网`,
    confirmText: '创建（下发 Kea）',
  });
  formModalApi.open();
}

async function add() {
  const f = form.value;
  if (!f.orgId) {
    message.warning('请选择所属组织');
    return;
  }
  if (!f.name) {
    message.warning('请填写子网名称');
    return;
  }
  if (!f.cidr) {
    message.warning('请填写 CIDR（如 10.61.172.0/24）');
    return;
  }
  let pools:
    | undefined
    | {
        delegatedLen?: number;
        endAddr?: string;
        kind: string;
        prefixLen?: number;
        startAddr: string;
      }[];
  if (
    f.family === 6 &&
    f.poolKind === 'pd' &&
    f.poolStart &&
    f.poolPrefixLen &&
    f.poolDelegatedLen
  ) {
    // PD 委派池：startAddr=委派前缀，endAddr 由后端推导（M2-018）
    pools = [
      {
        startAddr: f.poolStart,
        prefixLen: f.poolPrefixLen,
        delegatedLen: f.poolDelegatedLen,
        kind: 'pd',
      },
    ];
  } else if (f.poolStart && f.poolEnd) {
    pools = [
      {
        startAddr: f.poolStart,
        endAddr: f.poolEnd,
        kind: f.family === 6 ? f.poolKind : 'dynamic',
      },
    ];
  } else if (f.poolStart || f.poolEnd) {
    message.warning(
      '地址池需同时填写池起与池止（留空则不建池，该子网不分配动态地址）',
    );
    return;
  } else {
    message.info('未填地址池：该子网不分配动态地址（仅可用于保留/静态绑定）');
  }
  try {
    if (editingId.value) {
      await updateSubnet(editingId.value, {
        name: f.name,
        cidr: f.cidr,
        gateway: f.gateway || undefined,
        dnsServers: f.dnsServers || undefined,
        pools,
        validLifetime: f.validLifetime,
        options: f.options.filter((o) => o.enabled),
      });
      message.success('子网已更新并下发 Kea');
    } else {
      await createSubnet({
        orgId: f.orgId,
        name: f.name,
        family: f.family,
        cidr: f.cidr,
        pools,
        gateway: f.gateway || undefined,
        dnsServers: f.dnsServers || undefined,
        validLifetime: f.validLifetime,
        options: f.options.filter((o) => o.enabled),
      });
      message.success('子网已创建并下发 Kea');
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : '创建/下发失败（Kea 不可用或参数被拒）',
    );
    return; // 保留弹窗与已填内容，便于修正后重试
  }
  editingId.value = undefined;
  formModalApi.close();
  form.value = emptyForm(f.family, f.orgId);
  await loadAll();
}

function addOptionRow() {
  form.value.options.push({
    code: 0,
    name: '',
    data: '',
    csvFormat: true,
    enabled: true,
  });
}

function removeOptionRow(i: number) {
  form.value.options.splice(i, 1);
}

function onPresetChange(i: number, code: number) {
  const row = form.value.options[i];
  if (!row) return;
  row.code = code;
  const presets = form.value.family === 6 ? DHCP6_OPTION_PRESETS : DHCP4_OPTION_PRESETS;
  row.name = presets.find((x) => x.code === code)?.name ?? '';
}

function setOptionCode(i: number, code: number) {
  const row = form.value.options[i];
  if (row) row.code = code;
}

function edit(r: Subnet) {
  const p0 = (r.pools ?? [])[0];
  editingId.value = r.id;
  form.value = {
    orgId: r.orgId,
    name: r.name,
    family: r.family,
    cidr: r.cidr,
    gateway: r.gateway ?? '',
    dnsServers: r.dnsServers ?? '',
    poolStart: p0?.startAddr ?? '',
    poolEnd: p0?.endAddr ?? '',
    poolKind: (p0?.kind as 'dynamic' | 'pd') ?? 'dynamic',
    poolPrefixLen: p0?.prefixLen ?? 64,
    poolDelegatedLen: p0?.delegatedLen ?? 80,
    validLifetime: r.validLifetime ?? 3600,
    options: subnetOptions(r).map((o) => ({
      code: o.code,
      name: o.name ?? '',
      data: o.data,
      csvFormat: o.csvFormat ?? true,
      enabled: o.enabled ?? true,
    })),
  };
  formModalApi.setState({ title: '编辑子网', confirmText: '保存修改' });
  formModalApi.open();
}

async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteSubnet(id);
  } catch {
    // 引用保护/Kea 下发失败：detail 已由 request 层 message 展示
  }
  await loadAll();
}

// ── PD / NA 租约（Kea 实时） ──────────────────────────────────────────
const lease6Rows = ref<DhcpLease6Row[]>([]);
const lease6Loading = ref(false);

async function loadLeases6() {
  lease6Loading.value = true;
  try {
    const res = await listDhcpLeases6();
    lease6Rows.value = res.items ?? [];
  } catch {
    lease6Rows.value = [];
  } finally {
    lease6Loading.value = false;
  }
}

const lease6PdCount = computed(
  () => lease6Rows.value.filter((r) => r.leaseType === 'IA_PD').length,
);
const lease6NaCount = computed(
  () => lease6Rows.value.filter((r) => r.leaseType === 'IA_NA').length,
);

// ── 组织管理抽屉 ───────────────────────────────────────────────────────
const [OrgDrawer, orgDrawerApi] = useVbenDrawer({
  class: 'w-[620px]',
  footer: false,
  title: '组织架构管理',
  onClosed: () => void loadAll(),
});

// ── Vben Vxe Table：子网 ───────────────────────────────────────────────
const gridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '子网', minWidth: 250, slots: { default: 'subnet' } },
    { field: 'orgId', title: '所属组织', minWidth: 120, slots: { default: 'orgId' } },
    { field: 'pools', title: '地址池 / 委派', minWidth: 170, slots: { default: 'pools' } },
    { field: 'capacity', title: '容量', width: 84, slots: { default: 'capacity' } },
    { field: 'online', title: '在线 / 利用率', width: 120, slots: { default: 'util' } },
    { field: 'op', title: '操作', width: 96, fixed: 'right', slots: { default: 'op' } },
  ],
  emptyText: '当前组织下暂无子网，点击右上角「新建」添加',
  loading: loading.value,
  pagerConfig: { enabled: true },
  rowConfig: { height: 66, keyField: 'id' },
});
const [SubnetGrid, subnetGridApi] = useVbenVxeGrid({ gridOptions });

const lease6GridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'ipAddress', title: '地址 / 前缀', minWidth: 200, slots: { default: 'lease6Addr' } },
    { field: 'leaseType', title: '类型', width: 100, slots: { default: 'lease6Type' } },
    { field: 'prefixLen', title: '前缀长', width: 90 },
    { field: 'duid', title: 'DUID', minWidth: 190, showOverflow: 'tooltip' },
    { field: 'hwAddress', title: 'MAC', minWidth: 140 },
    { field: 'validLifetime', title: '有效期', width: 90, slots: { default: 'lease6Life' } },
  ],
  emptyText: 'Kea 暂未返回 DHCPv6 租约',
  loading: lease6Loading.value,
  pagerConfig: { enabled: true },
  rowConfig: { keyField: 'ipAddress' },
});
const [Lease6Grid] = useVbenVxeGrid({ gridOptions: lease6GridOptions });

// ── 初始化 ─────────────────────────────────────────────────────────────
onMounted(async () => {
  if (route.query.family === '6' || route.query.family === 'v6') {
    familyTab.value = 'v6';
  }
  if (typeof route.query.org === 'string') filterOrgId.value = route.query.org;
  await loadAll();
  void loadLeases6();
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:network"
      title="子网管理"
      description="按组织维护 IPv4 / IPv6 网段、地址池与 DHCP 下发参数，变更后自动同步 Kea。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layers" class="size-3.5" />
          {{ allRows.length }} 个子网 · {{ orgTreeData.length }} 个根组织
        </span>
        <Button :loading="loading" @click="loadAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          刷新
        </Button>
        <Button type="primary" @click="openAdd(familyTab === 'v6' ? 6 : 4)">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />
          新建 {{ familyTab === 'v6' ? 'IPv6' : 'IPv4' }} 子网
        </Button>
      </template>
    </PageHeader>

    <!-- 全局指标 -->
    <div class="ipam-stat-grid">
      <StatCard
        label="IPv4 子网"
        :value="v4Count"
        unit="个"
        icon="lucide:network"
        tone="blue"
        :hint="`${v4PoolCount} 个地址池 · 在线 ${onlineTotals.v4} 条`"
      />
      <StatCard
        label="IPv6 子网"
        :value="v6Count"
        unit="个"
        icon="lucide:globe"
        tone="violet"
        :hint="`${v6PdCount} 个 PD 委派 · 在线 ${onlineTotals.v6} 条`"
      />
      <StatCard
        label="在线租约"
        :value="totalOnline"
        unit="条"
        icon="lucide:radio"
        tone="green"
        :hint="`IPv4 ${onlineTotals.v4} · IPv6 ${onlineTotals.v6}`"
      />
      <StatCard
        label="Kea 下发覆盖"
        :value="`${issuedCount}/${allRows.length}`"
        icon="lucide:cloud-upload"
        :tone="pendingKea > 0 ? 'amber' : 'cyan'"
        :progress="keaRatio"
        :hint="
          pendingKea > 0 ? `${pendingKea} 个子网待下发` : '全部子网已同步至 Kea'
        "
      />
    </div>

    <div class="ipam-subnets-layout">
      <!-- 组织架构 -->
      <Card class="ipam-org-panel">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:building-2" />
            组织架构
          </span>
        </template>
        <template #extra>
          <Button size="small" type="text" @click="orgDrawerApi.open()">
            <IconifyIcon icon="lucide:settings-2" class="mr-1 size-3.5" />
            管理
          </Button>
        </template>

        <Input
          v-model:value="orgSearch"
          allow-clear
          placeholder="搜索组织名称"
          size="small"
        >
          <template #prefix>
            <IconifyIcon icon="lucide:search" class="size-3.5 text-gray-400" />
          </template>
        </Input>

        <button
          class="ipam-org-all"
          type="button"
          :data-active="!filterOrgId"
          @click="selectOrg('')"
        >
          <span class="ipam-org-all__label">
            <IconifyIcon icon="lucide:layers" class="size-4" />
            全部组织
          </span>
          <span class="ipam-org-badge">{{ allRows.length }}</span>
        </button>

        <Tree
          v-if="orgTreeData.length > 0"
          class="ipam-org-tree"
          :tree-data="orgTreeData"
          :expanded-keys="effectiveExpandedKeys"
          :selected-keys="filterOrgId ? [filterOrgId] : []"
          block-node
          @expand="onOrgExpand"
          @select="onOrgSelect"
        >
          <template #title="{ key, title }">
            <span class="ipam-org-node">
              <span class="ipam-org-node__name">{{ title }}</span>
              <span class="ipam-org-node__stats">
                <span class="ipam-org-badge">{{ statsOf(String(key)).subnets }}</span>
                <span class="ipam-org-badge ipam-org-badge--online">
                  {{ statsOf(String(key)).online }}
                </span>
              </span>
            </span>
          </template>
        </Tree>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">
            {{ orgSearch ? '没有匹配的组织' : '尚未创建组织' }}
          </div>
          <div class="ipam-empty__desc">
            {{ orgSearch ? '换个关键词试试' : '点击右上角「管理」创建组织节点' }}
          </div>
        </div>

        <div class="ipam-org-panel__foot">
          <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
          <span>
            {{
              filterOrgId
                ? `当前范围：${orgScopeLabel}（含下级组织）`
                : '统计含下级组织，点选节点可下钻'
            }}
          </span>
        </div>
      </Card>

      <div class="ipam-subnets-main">
        <!-- 双族切换 -->
        <div class="ipam-fam-switch">
          <button
            v-for="f in familyCards"
            :key="f.key"
            class="ipam-fam-card"
            type="button"
            :data-active="familyTab === f.key"
            :data-family="f.key"
            @click="switchFamily(f.key)"
          >
            <span class="ipam-fam-card__icon">
              <IconifyIcon :icon="f.icon" class="size-[18px]" />
            </span>
            <span class="ipam-fam-card__body">
              <span class="ipam-fam-card__title">
                {{ f.title }}
                <em class="ipam-fam-card__tag">{{ f.tag }}</em>
              </span>
              <span class="ipam-fam-card__stats">
                <span><b>{{ f.subnets }}</b> 子网</span>
                <span><b>{{ f.online }}</b> 在线</span>
                <span><b>{{ f.issued }}</b> 已下发</span>
              </span>
            </span>
            <span class="ipam-fam-card__check">
              <IconifyIcon icon="lucide:check" class="size-3.5" />
            </span>
          </button>
        </div>

        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon
                :icon="familyTab === 'v6' ? 'lucide:globe' : 'lucide:network'"
              />
              {{ familyTab === 'v6' ? 'IPv6 子网' : 'IPv4 子网' }}
              <em class="ipam-card-sub">
                {{ orgScopeLabel }} · {{ visibleRows.length }} 个
              </em>
            </span>
          </template>
          <template #extra>
            <div class="ipam-card-tools">
              <span class="ipam-chip" :data-tone="familyTab === 'v6' ? 'violet' : 'blue'">
                {{ familyTab === 'v6' ? '前缀 / PD 委派' : '逐地址规划' }}
              </span>
              <Button
                size="small"
                type="primary"
                @click="openAdd(familyTab === 'v6' ? 6 : 4)"
              >
                <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />
                新建
              </Button>
            </div>
          </template>

          <div class="ipam-summary">
            <div v-for="item in summaryItems" :key="item.label" class="ipam-summary__item">
              <span class="ipam-summary__label">{{ item.label }}</span>
              <span class="ipam-summary__value">
                {{ item.value }}<small v-if="item.unit">{{ item.unit }}</small>
              </span>
            </div>
          </div>

          <SubnetGrid class="mt-4" :table-data="visibleRows">
            <template #subnet="{ row }">
              <div class="ipam-subnet-cell">
                <span class="ipam-subnet-cell__title">
                  <i class="ipam-fam-dot" :data-family="row.family"></i>
                  <span class="ipam-subnet-cell__name">
                    {{ row.name || '未命名子网' }}
                  </span>
                  <span
                    v-if="row.keaSubnetId"
                    class="ipam-chip"
                    data-tone="green"
                    :title="`Kea subnet-id ${row.keaSubnetId}`"
                  >
                    Kea #{{ row.keaSubnetId }}
                  </span>
                  <span v-else class="ipam-chip" data-tone="amber">未下发</span>
                </span>
                <a
                  class="ipam-link ipam-mono"
                  :href="`/dhcp/ledger/${row.family === 6 ? 'v6' : 'v4'}?cidr=${row.cidr}`"
                  @click.prevent="
                    router.push(
                      `/dhcp/ledger/${row.family === 6 ? 'v6' : 'v4'}?cidr=${row.cidr}`,
                    )
                  "
                >
                  {{ row.cidr }}
                  <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
                </a>
                <span class="ipam-subnet-cell__meta">
                  <template v-if="row.family === 4">
                    网关 {{ row.gateway || '未配置' }} · DNS
                    {{ row.dnsServers || '未配置' }}
                  </template>
                  <template v-else>
                    DNS {{ row.dnsServers || '未配置' }} · 网关走 RA
                  </template>
                  · 租约 {{ fmtLifetime(row.validLifetime) }}
                </span>
              </div>
            </template>

            <template #orgId="{ row }">
              <span class="ipam-org-path" :title="orgPathOf(row.orgId).join(' / ')">
                <template v-for="(seg, i) in orgPathOf(row.orgId)" :key="`${seg}-${i}`">
                  <IconifyIcon
                    v-if="i > 0"
                    icon="lucide:chevron-right"
                    class="size-3"
                  />
                  <b v-if="i === orgPathOf(row.orgId).length - 1">{{ seg }}</b>
                  <span v-else>{{ seg }}</span>
                </template>
                <span v-if="orgPathOf(row.orgId).length === 0">—</span>
              </span>
            </template>

            <template #pools="{ row }">
              <div class="ipam-pool">
                <span class="ipam-pool__chips">
                  <span
                    v-for="kind in poolKinds(row as Subnet)"
                    :key="kind.label"
                    class="ipam-chip"
                    :data-tone="kind.tone"
                  >
                    {{ kind.label }}
                  </span>
                  <span
                    v-if="poolKinds(row as Subnet).length === 0"
                    class="ipam-chip"
                    data-tone="muted"
                  >
                    未配置
                  </span>
                </span>
                <span
                  v-if="poolSummary(row as Subnet)"
                  class="ipam-pool__range"
                  :title="poolSummary(row as Subnet)"
                >
                  {{ poolSummary(row as Subnet) }}
                </span>
              </div>
            </template>

            <template #capacity="{ row }">
              <div class="ipam-capacity">
                <template v-if="subnetCapacity(row as Subnet) !== null">
                  <span class="ipam-capacity__value">
                    {{ fmtBig(subnetCapacity(row as Subnet)) }}
                  </span>
                  <span class="ipam-capacity__unit">
                    {{ capacityUnit([row as Subnet]) }}
                  </span>
                </template>
                <span v-else class="ipam-capacity__value ipam-capacity__value--empty">
                  —
                </span>
              </div>
            </template>

            <template #util="{ row }">
              <div class="ipam-util">
                <div class="ipam-util__head">
                  <span class="ipam-util__used">
                    {{ onlineBySubnet[row.id] ?? 0 }}
                    <small>在线</small>
                  </span>
                  <span class="ipam-util__pct">
                    <template v-if="utilizationOf(row as Subnet) !== null">
                      {{ utilizationOf(row as Subnet)?.toFixed(1) }}%
                    </template>
                    <template v-else>—</template>
                  </span>
                </div>
                <div
                  class="ipam-progress"
                  :data-tone="toneOfPct(utilizationOf(row as Subnet))"
                >
                  <i
                    :style="{
                      width: `${Math.max(utilizationOf(row as Subnet) ?? 0, utilizationOf(row as Subnet) ? 2 : 0)}%`,
                    }"
                  ></i>
                </div>
              </div>
            </template>

            <template #op="{ row }">
              <div class="ipam-op-group">
                <Button
                  class="ipam-op-btn"
                  size="small"
                  title="地址规划"
                  type="text"
                  @click="
                    router.push(
                      `/dhcp/ledger/${row.family === 6 ? 'v6' : 'v4'}?cidr=${row.cidr}`,
                    )
                  "
                >
                  <IconifyIcon icon="lucide:map" class="size-3.5" />
                </Button>
                <Button
                  class="ipam-op-btn"
                  size="small"
                  title="编辑"
                  type="text"
                  @click="edit(row as Subnet)"
                >
                  <IconifyIcon icon="lucide:pencil" class="size-3.5" />
                </Button>
                <Popconfirm
                  title="确认删除该子网？"
                  description="删除后不可恢复；已被租约/保留引用时会失败。"
                  ok-text="删除"
                  cancel-text="取消"
                  @confirm="remove(row.id)"
                >
                  <Button
                    class="ipam-op-btn"
                    danger
                    size="small"
                    title="删除"
                    type="text"
                  >
                    <IconifyIcon icon="lucide:trash-2" class="size-3.5" />
                  </Button>
                </Popconfirm>
              </div>
            </template>
          </SubnetGrid>

          <!-- IPv6：Kea 实时租约 -->
          <template v-if="familyTab === 'v6'">
            <Divider class="!my-5" />
            <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:radio-tower" />
                DHCPv6 实时租约
                <em class="ipam-card-sub">
                  Kea lease_cmds · NA {{ lease6NaCount }} / PD {{ lease6PdCount }}
                </em>
              </span>
              <Button size="small" :loading="lease6Loading" @click="loadLeases6">
                <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
                刷新租约
              </Button>
            </div>
            <div class="ipam-note mb-3">
              <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
              <span>
                Kea 实时投影（lease_cmds），与上方台账在线数存在秒级差异；PD 委派以 IA_PD 前缀为单位。
              </span>
            </div>
            <Lease6Grid :table-data="lease6Rows">
              <template #lease6Addr="{ row }">
                <span class="ipam-mono">{{ row.ipAddress }}</span>
              </template>
              <template #lease6Type="{ row }">
                <span
                  class="ipam-chip"
                  :data-tone="row.leaseType === 'IA_PD' ? 'violet' : 'blue'"
                >
                  {{ row.leaseType }}
                </span>
              </template>
              <template #lease6Life="{ row }">
                <span class="ipam-hint">{{ fmtLifetime(row.validLifetime) }}</span>
              </template>
            </Lease6Grid>
          </template>
        </Card>
      </div>
    </div>

    <OrgDrawer>
      <OrgManageCard />
    </OrgDrawer>

    <FormModal class="w-[900px]">
      <!-- 基本信息 -->
      <Divider plain style="margin: 4px 0 12px" orientation="left">
        基本信息
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <div class="mb-1 text-xs text-gray-400">组织</div>
          <Select
            v-model:value="form.orgId"
            placeholder="选择组织节点"
            style="width: 240px"
            :options="orgOptions.map((o) => ({ label: o.label, value: o.value }))"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">名称</div>
          <Input
            v-model:value="form.name"
            placeholder="研发-办公"
            style="width: 160px"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">
            CIDR
            <Tag class="ml-1" :color="isV6 ? 'geekblue' : 'blue'">
              IPv{{ form.family }}
            </Tag>
          </div>
          <Input
            v-model:value="form.cidr"
            style="width: 220px"
            :placeholder="isV6 ? '2406:172::/64' : '10.61.40.0/24'"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">租约时长（秒）</div>
          <div class="flex items-center gap-2">
            <InputNumber
              v-model:value="form.validLifetime"
              :max="2592000"
              :min="60"
              style="width: 120px"
            />
            <Select
              placeholder="预设"
              style="width: 92px"
              :options="[
                { label: '1小时', value: 3600 },
                { label: '8小时', value: 28800 },
                { label: '1天', value: 86400 },
                { label: '7天', value: 604800 },
              ]"
              :value="
                [3600, 28800, 86400, 604800].includes(form.validLifetime)
                  ? form.validLifetime
                  : undefined
              "
              @change="(v: unknown) => (form.validLifetime = Number(v))"
            />
          </div>
        </div>
      </div>

      <!-- 地址池 -->
      <Divider plain style="margin: 16px 0 12px" orientation="left">
        地址池
        <span class="text-xs text-gray-400">
          （留空则不分配动态地址，仅可保留/静态绑定）
        </span>
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <template v-if="isV6">
          <div>
            <div class="mb-1 text-xs text-gray-400">池类型</div>
            <Select
              v-model:value="form.poolKind"
              style="width: 120px"
              :options="[
                { label: '地址池', value: 'dynamic' },
                { label: 'PD 委派', value: 'pd' },
              ]"
            />
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">
              {{ form.poolKind === 'pd' ? '委派前缀' : '池起' }}
            </div>
            <Input
              v-model:value="form.poolStart"
              style="width: 200px"
              :placeholder="
                form.poolKind === 'pd' ? '2001:db8:1::' : '2406:172::100'
              "
            />
          </div>
          <template v-if="form.poolKind === 'pd'">
            <div>
              <div class="mb-1 text-xs text-gray-400">前缀长度</div>
              <InputNumber
                v-model:value="form.poolPrefixLen"
                :max="128"
                :min="1"
                style="width: 90px"
              />
            </div>
            <div>
              <div class="mb-1 text-xs text-gray-400">委派长度</div>
              <InputNumber
                v-model:value="form.poolDelegatedLen"
                :max="128"
                :min="1"
                style="width: 90px"
              />
            </div>
          </template>
          <div v-else>
            <div class="mb-1 text-xs text-gray-400">池止</div>
            <Input
              v-model:value="form.poolEnd"
              placeholder="2406:172::200"
              style="width: 170px"
            />
          </div>
        </template>
        <template v-else>
          <div>
            <div class="mb-1 text-xs text-gray-400">池起</div>
            <Input
              v-model:value="form.poolStart"
              placeholder="10.61.172.100"
              style="width: 170px"
            />
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">池止</div>
            <Input
              v-model:value="form.poolEnd"
              placeholder="10.61.172.200"
              style="width: 170px"
            />
          </div>
        </template>
      </div>

      <!-- 网关与 DNS -->
      <Divider plain style="margin: 16px 0 12px" orientation="left">
        网关与 DNS
        <span class="text-xs text-gray-400">（IPv6 网关走 RA，此处无需填写）</span>
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <div v-if="!isV6">
          <div class="mb-1 text-xs text-gray-400">网关</div>
          <Input
            v-model:value="form.gateway"
            placeholder="10.61.40.1"
            style="width: 180px"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">DNS 服务器</div>
          <Input
            v-model:value="form.dnsServers"
            style="width: 260px"
            :placeholder="
              isV6 ? '2406:172::53' : '223.5.5.5, 114.114.114.114'
            "
          />
        </div>
      </div>

      <!-- DHCP 选项 -->
      <Divider plain style="margin: 16px 0 12px" orientation="left">
        DHCP 选项 <span class="text-xs text-gray-400">（子网级覆盖全局）</span>
      </Divider>
      <div class="mb-2 flex items-center gap-2">
        <Button size="small" @click="addOptionRow">+ 添加选项</Button>
      </div>
      <div
        v-for="(o, i) in form.options"
        :key="i"
        class="mb-2 flex flex-wrap items-center gap-2"
      >
        <Select
          placeholder="预设选项（RFC 标准）"
          style="width: 240px"
          :options="
            (isV6 ? DHCP6_OPTION_PRESETS : DHCP4_OPTION_PRESETS).map((x) => ({
              label: `选项${x.code} ${x.label}`,
              value: x.code,
            }))
          "
          :value="o.code || undefined"
          @change="(v: unknown) => onPresetChange(i, Number(v))"
        />
        <span class="text-xs text-gray-400">或 code</span>
        <InputNumber
          style="width: 90px"
          :max="isV6 ? 65535 : 254"
          :min="1"
          :value="o.code"
          @change="(v: unknown) => setOptionCode(i, Number(v))"
        />
        <Input
          v-model:value="o.data"
          placeholder="选项值（多值逗号分隔）"
          style="width: 240px"
        />
        <span class="text-xs text-gray-400">CSV</span>
        <Switch v-model:checked="o.csvFormat" size="small" />
        <Button danger size="small" @click="removeOptionRow(i)">移除</Button>
      </div>
      <div v-if="form.options.length === 0" class="text-xs text-gray-400">
        未配置子网级选项——网关/DNS 用上方字段；其余选项可在此添加（如 NTP、域名后缀、PXE）。
      </div>
    </FormModal>
  </div>
</template>
