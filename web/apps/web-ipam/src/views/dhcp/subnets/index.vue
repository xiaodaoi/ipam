<script setup lang="ts">
import type { VxeGridProps } from '@vben/plugins/vxe-table';

import type {
  DhcpLease6Row,
  OrgTreeNode,
  Subnet,
  SubnetOptionIn,
} from '#/api/ipam';

import { computed, onMounted, ref, watch } from 'vue';
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
import { $t } from '#/locales';

import { getDhcp4OptionPresets, getDhcp6OptionPresets } from './options';

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
    ? orgPathOf(filterOrgId.value).join(' / ') || $t('dhcp.subnets.allOrgs')
    : $t('dhcp.subnets.allOrgs'),
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
      tag:
        key === 'v4'
          ? $t('dhcp.subnets.family.v4Tag')
          : $t('dhcp.subnets.family.v6Tag'),
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
    {
      label: $t('dhcp.subnets.summary.subnets'),
      value: String(list.length),
      unit: $t('dhcp.common.unitCount'),
    },
  ];
  if (familyTab.value === 'v6') {
    items.push(
      {
        label: $t('dhcp.subnets.summary.pdPools'),
        value: String(pdPools),
        unit: $t('dhcp.common.unitCount'),
      },
      {
        label: $t('dhcp.subnets.summary.dynamicPools'),
        value: String(dynamicPools),
        unit: $t('dhcp.common.unitCount'),
      },
      {
        label: $t('dhcp.subnets.summary.capacity'),
        value: fmtBig(capacity),
        unit: capacityUnit(list),
      },
    );
  } else {
    items.push(
      {
        label: $t('dhcp.subnets.summary.dynamicPools'),
        value: String(dynamicPools),
        unit: $t('dhcp.common.unitCount'),
      },
      {
        label: $t('dhcp.subnets.summary.addressCapacity'),
        value: fmtBig(capacity),
        unit: $t('dhcp.common.unitAddress'),
      },
      {
        label: $t('dhcp.subnets.summary.avgUtil'),
        value: capacity > B0 ? utilizationText(online, capacity) : '—',
        unit: capacity > B0 ? '%' : '',
      },
    );
  }
  items.push(
    {
      label: $t('dhcp.subnets.summary.onlineLeases'),
      value: String(online),
      unit: $t('dhcp.common.unitRecord'),
    },
    {
      label: $t('dhcp.subnets.summary.keaIssued'),
      value: `${issued}/${list.length}`,
      unit: '',
    },
  );
  return items;
});

function capacityUnit(list: Subnet[]): string {
  if (familyTab.value !== 'v6') return $t('dhcp.common.unitAddress');
  const hasPd = list.some((r) => (r.pools ?? []).some((p) => p.kind === 'pd'));
  const hasDynamic = list.some((r) =>
    (r.pools ?? []).some((p) => p.kind !== 'pd'),
  );
  if (hasPd && hasDynamic) return $t('dhcp.common.unitCount');
  return hasPd
    ? $t('dhcp.common.unitPrefix')
    : $t('dhcp.common.unitAddress');
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
  if (sec % 86_400 === 0)
    return $t('dhcp.common.durationDays', { n: sec / 86_400 });
  if (sec % 3600 === 0)
    return $t('dhcp.common.durationHours', { n: sec / 3600 });
  if (sec % 60 === 0)
    return $t('dhcp.common.durationMinutes', { n: sec / 60 });
  return $t('dhcp.common.durationSeconds', { n: sec });
}

// ── 地址池展示 ─────────────────────────────────────────────────────────
const POOL_KIND = computed<Record<string, { label: string; tone: string }>>(
  () => ({
    dynamic: { label: $t('dhcp.subnets.pool.dynamic'), tone: 'blue' },
    excluded: { label: $t('dhcp.subnets.pool.excluded'), tone: 'muted' },
    pd: { label: $t('dhcp.subnets.pool.pd'), tone: 'violet' },
  }),
);

function poolKinds(r: Subnet): { label: string; tone: string }[] {
  const seen = new Set<string>();
  const out: { label: string; tone: string }[] = [];
  for (const pool of r.pools ?? []) {
    if (seen.has(pool.kind)) continue;
    seen.add(pool.kind);
    out.push(POOL_KIND.value[pool.kind] ?? { label: pool.kind, tone: 'muted' });
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
  return pools.length > 1
    ? $t('dhcp.subnets.pool.more', { one, n: pools.length - 1 })
    : one;
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
  title: $t('dhcp.subnets.form.addTitle'),
  confirmText: $t('dhcp.subnets.form.confirmCreate'),
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
    vlan: '',
    site: '',
    purpose: '',
    owner: '',
  };
}

const form = ref(emptyForm(4));
const isV6 = computed(() => form.value.family === 6);

const lifetimePresets = computed(() => [
  { label: $t('dhcp.subnets.form.preset1h'), value: 3600 },
  { label: $t('dhcp.subnets.form.preset8h'), value: 28_800 },
  { label: $t('dhcp.subnets.form.preset1d'), value: 86_400 },
  { label: $t('dhcp.subnets.form.preset7d'), value: 604_800 },
]);

const poolKindOptions = computed(() => [
  { label: $t('dhcp.subnets.form.poolDynamic'), value: 'dynamic' },
  { label: $t('dhcp.subnets.form.poolPd'), value: 'pd' },
]);

const optionPresetOptions = computed(() =>
  (isV6.value ? getDhcp6OptionPresets() : getDhcp4OptionPresets()).map((x) => ({
    label: $t('dhcp.subnets.form.presetLabel', {
      code: x.code,
      label: x.label,
    }),
    value: x.code,
  })),
);

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
    title: $t('dhcp.subnets.form.newTitle', { family }),
    confirmText: $t('dhcp.subnets.form.confirmCreate'),
  });
  formModalApi.open();
}

async function add() {
  const f = form.value;
  if (!f.orgId) {
    message.warning($t('dhcp.subnets.form.needOrg'));
    return;
  }
  if (!f.name) {
    message.warning($t('dhcp.subnets.form.needName'));
    return;
  }
  if (!f.cidr) {
    message.warning($t('dhcp.subnets.form.needCidr'));
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
    message.warning($t('dhcp.subnets.form.poolPair'));
    return;
  } else {
    message.info($t('dhcp.subnets.form.noPool'));
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
        vlan: f.vlan,
        site: f.site,
        purpose: f.purpose,
        owner: f.owner,
      });
      message.success($t('dhcp.subnets.form.updated'));
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
        vlan: f.vlan,
        site: f.site,
        purpose: f.purpose,
        owner: f.owner,
      });
      message.success($t('dhcp.subnets.form.created'));
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.subnets.form.failed'),
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
  const presets =
    form.value.family === 6 ? getDhcp6OptionPresets() : getDhcp4OptionPresets();
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
    vlan: r.vlan ?? '',
    site: r.site ?? '',
    purpose: r.purpose ?? '',
    owner: r.owner ?? '',
    options: subnetOptions(r).map((o) => ({
      code: o.code,
      name: o.name ?? '',
      data: o.data,
      csvFormat: o.csvFormat ?? true,
      enabled: o.enabled ?? true,
    })),
  };
  formModalApi.setState({
    title: $t('dhcp.subnets.form.editTitle'),
    confirmText: $t('dhcp.subnets.form.saveEdit'),
  });
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
  title: $t('dhcp.subnets.orgDrawer'),
  onClosed: () => void loadAll(),
});

// ── Vben Vxe Table：子网 ───────────────────────────────────────────────
const gridOptions = computed<VxeGridProps>(() => ({
  columns: [
    {
      field: 'name',
      title: $t('dhcp.subnets.grid.name'),
      minWidth: 250,
      slots: { default: 'subnet' },
    },
    {
      field: 'orgId',
      title: $t('dhcp.subnets.grid.org'),
      minWidth: 120,
      slots: { default: 'orgId' },
    },
    {
      field: 'pools',
      title: $t('dhcp.subnets.grid.pools'),
      minWidth: 170,
      slots: { default: 'pools' },
    },
    {
      field: 'capacity',
      title: $t('dhcp.subnets.grid.capacity'),
      width: 84,
      slots: { default: 'capacity' },
    },
    {
      field: 'online',
      title: $t('dhcp.subnets.grid.util'),
      width: 120,
      slots: { default: 'util' },
    },
    {
      field: 'op',
      title: $t('dhcp.subnets.grid.op'),
      width: 96,
      fixed: 'right',
      slots: { default: 'op' },
    },
  ],
  emptyText: $t('dhcp.subnets.grid.empty'),
  loading: loading.value,
  pagerConfig: { enabled: true },
  rowConfig: { height: 66, keyField: 'id' },
}));
const [SubnetGrid, subnetGridApi] = useVbenVxeGrid({
  gridOptions: gridOptions.value,
});
// 语言切换后同步表头与空状态文案
watch(gridOptions, (opts) => subnetGridApi.setState(opts as any));

const lease6GridOptions = computed<VxeGridProps>(() => ({
  columns: [
    {
      field: 'ipAddress',
      title: $t('dhcp.subnets.lease6.addr'),
      minWidth: 200,
      slots: { default: 'lease6Addr' },
    },
    {
      field: 'leaseType',
      title: $t('dhcp.subnets.lease6.type'),
      width: 100,
      slots: { default: 'lease6Type' },
    },
    { field: 'prefixLen', title: $t('dhcp.subnets.lease6.prefixLen'), width: 90 },
    { field: 'duid', title: 'DUID', minWidth: 190, showOverflow: 'tooltip' },
    { field: 'hwAddress', title: 'MAC', minWidth: 140 },
    {
      field: 'validLifetime',
      title: $t('dhcp.subnets.lease6.life'),
      width: 90,
      slots: { default: 'lease6Life' },
    },
  ],
  emptyText: $t('dhcp.subnets.lease6.empty'),
  loading: lease6Loading.value,
  pagerConfig: { enabled: true },
  rowConfig: { keyField: 'ipAddress' },
}));
const [Lease6Grid, lease6GridApi] = useVbenVxeGrid({
  gridOptions: lease6GridOptions.value,
});
watch(lease6GridOptions, (opts) => lease6GridApi.setState(opts as any));

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
      :title="$t('dhcp.subnets.title')"
      :description="$t('dhcp.subnets.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layers" class="size-3.5" />
          {{
            $t('dhcp.subnets.meta', {
              subnets: allRows.length,
              orgs: orgTreeData.length,
            })
          }}
        </span>
        <Button :loading="loading" @click="loadAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          {{ $t('dhcp.common.refresh') }}
        </Button>
        <Button type="primary" @click="openAdd(familyTab === 'v6' ? 6 : 4)">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />
          {{
            $t('dhcp.subnets.newSubnet', {
              family: familyTab === 'v6' ? 'IPv6' : 'IPv4',
            })
          }}
        </Button>
      </template>
    </PageHeader>

    <!-- 全局指标 -->
    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dhcp.subnets.stat.v4Label')"
        :value="v4Count"
        :unit="$t('dhcp.common.unitCount')"
        icon="lucide:network"
        tone="blue"
        :hint="
          $t('dhcp.subnets.stat.v4Hint', {
            pools: v4PoolCount,
            online: onlineTotals.v4,
          })
        "
      />
      <StatCard
        :label="$t('dhcp.subnets.stat.v6Label')"
        :value="v6Count"
        :unit="$t('dhcp.common.unitCount')"
        icon="lucide:globe"
        tone="violet"
        :hint="
          $t('dhcp.subnets.stat.v6Hint', {
            pd: v6PdCount,
            online: onlineTotals.v6,
          })
        "
      />
      <StatCard
        :label="$t('dhcp.subnets.stat.onlineLabel')"
        :value="totalOnline"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:radio"
        tone="green"
        :hint="`IPv4 ${onlineTotals.v4} · IPv6 ${onlineTotals.v6}`"
      />
      <StatCard
        :label="$t('dhcp.subnets.stat.keaLabel')"
        :value="`${issuedCount}/${allRows.length}`"
        icon="lucide:cloud-upload"
        :tone="pendingKea > 0 ? 'amber' : 'cyan'"
        :progress="keaRatio"
        :hint="
          pendingKea > 0
            ? $t('dhcp.subnets.stat.keaPending', { n: pendingKea })
            : $t('dhcp.subnets.stat.keaDone')
        "
      />
    </div>

    <div class="ipam-subnets-layout">
      <!-- 组织架构 -->
      <Card class="ipam-org-panel">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:building-2" />
            {{ $t('dhcp.subnets.org.title') }}
          </span>
        </template>
        <template #extra>
          <Button size="small" type="text" @click="orgDrawerApi.open()">
            <IconifyIcon icon="lucide:settings-2" class="mr-1 size-3.5" />
            {{ $t('dhcp.subnets.org.manage') }}
          </Button>
        </template>

        <Input
          v-model:value="orgSearch"
          allow-clear
          :placeholder="$t('dhcp.subnets.org.searchPlaceholder')"
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
            {{ $t('dhcp.subnets.allOrgs') }}
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
            {{
              orgSearch
                ? $t('dhcp.subnets.org.emptyNoMatch')
                : $t('dhcp.subnets.org.emptyNone')
            }}
          </div>
          <div class="ipam-empty__desc">
            {{
              orgSearch
                ? $t('dhcp.subnets.org.emptyNoMatchDesc')
                : $t('dhcp.subnets.org.emptyNoneDesc')
            }}
          </div>
        </div>

        <div class="ipam-org-panel__foot">
          <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
          <span>
            {{
              filterOrgId
                ? $t('dhcp.subnets.scopeCurrent', { scope: orgScopeLabel })
                : $t('dhcp.subnets.scopeHint')
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
                <span><b>{{ f.subnets }}</b>
                  {{ $t('dhcp.subnets.family.subnetsUnit') }}</span>
                <span><b>{{ f.online }}</b>
                  {{ $t('dhcp.subnets.family.onlineUnit') }}</span>
                <span><b>{{ f.issued }}</b>
                  {{ $t('dhcp.subnets.family.issuedUnit') }}</span>
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
              {{
                familyTab === 'v6'
                  ? $t('dhcp.subnets.family.v6Title')
                  : $t('dhcp.subnets.family.v4Title')
              }}
              <em class="ipam-card-sub">
                {{
                  $t('dhcp.subnets.family.subtle', {
                    scope: orgScopeLabel,
                    n: visibleRows.length,
                  })
                }}
              </em>
            </span>
          </template>
          <template #extra>
            <div class="ipam-card-tools">
              <span class="ipam-chip" :data-tone="familyTab === 'v6' ? 'violet' : 'blue'">
                {{
                  familyTab === 'v6'
                    ? $t('dhcp.subnets.family.v6Tag')
                    : $t('dhcp.subnets.family.v4Tag')
                }}
              </span>
              <Button
                size="small"
                type="primary"
                @click="openAdd(familyTab === 'v6' ? 6 : 4)"
              >
                <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />
                {{ $t('dhcp.subnets.createShort') }}
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
                    {{ row.name || $t('dhcp.subnets.grid.unnamed') }}
                  </span>
                  <span
                    v-if="row.keaSubnetId"
                    class="ipam-chip"
                    data-tone="green"
                    :title="`Kea subnet-id ${row.keaSubnetId}`"
                  >
                    Kea #{{ row.keaSubnetId }}
                  </span>
                  <span v-else class="ipam-chip" data-tone="amber">
                    {{ $t('dhcp.subnets.grid.notIssued') }}
                  </span>
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
                    {{
                      $t('dhcp.subnets.grid.gateway', {
                        value:
                          row.gateway || $t('dhcp.common.notConfigured'),
                        dns:
                          row.dnsServers || $t('dhcp.common.notConfigured'),
                      })
                    }}
                  </template>
                  <template v-else>
                    {{
                      $t('dhcp.subnets.grid.dns', {
                        value:
                          row.dnsServers || $t('dhcp.common.notConfigured'),
                      })
                    }}
                  </template>
                  {{ $t('dhcp.subnets.grid.lease', { value: fmtLifetime(row.validLifetime) }) }}
                </span>
                <span
                  v-if="row.vlan || row.site || row.purpose || row.owner"
                  class="ipam-subnet-cell__meta"
                >
                  <span v-if="row.vlan" class="ipam-chip" data-tone="violet">
                    VLAN {{ row.vlan }}
                  </span>
                  <template v-if="row.site">
                    {{ $t('dhcp.subnets.grid.site', { value: row.site }) }}
                  </template>
                  <template v-if="row.purpose">
                    {{ $t('dhcp.subnets.grid.purpose', { value: row.purpose }) }}
                  </template>
                  <template v-if="row.owner">
                    {{ $t('dhcp.subnets.grid.owner', { value: row.owner }) }}
                  </template>
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
                    {{ $t('dhcp.common.notConfigured') }}
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
                    <small>{{ $t('dhcp.subnets.grid.onlineUnit') }}</small>
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
                  :title="$t('dhcp.subnets.grid.addrPlan')"
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
                  :title="$t('dhcp.common.edit')"
                  type="text"
                  @click="edit(row as Subnet)"
                >
                  <IconifyIcon icon="lucide:pencil" class="size-3.5" />
                </Button>
                <Popconfirm
                  :title="$t('dhcp.subnets.grid.confirmDelete')"
                  :description="$t('dhcp.subnets.grid.confirmDeleteDesc')"
                  :ok-text="$t('dhcp.common.delete')"
                  :cancel-text="$t('dhcp.common.cancel')"
                  @confirm="remove(row.id)"
                >
                  <Button
                    class="ipam-op-btn"
                    danger
                    size="small"
                    :title="$t('dhcp.common.delete')"
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
                {{ $t('dhcp.subnets.lease6.title') }}
                <em class="ipam-card-sub">
                  Kea lease_cmds · NA {{ lease6NaCount }} / PD {{ lease6PdCount }}
                </em>
              </span>
              <Button size="small" :loading="lease6Loading" @click="loadLeases6">
                <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
                {{ $t('dhcp.subnets.lease6.refresh') }}
              </Button>
            </div>
            <div class="ipam-note mb-3">
              <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
              <span>
                {{ $t('dhcp.subnets.lease6.note') }}
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
        {{ $t('dhcp.subnets.form.basic') }}
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.org') }}
          </div>
          <Select
            v-model:value="form.orgId"
            :placeholder="$t('dhcp.subnets.form.orgPlaceholder')"
            style="width: 240px"
            :options="orgOptions.map((o) => ({ label: o.label, value: o.value }))"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.name') }}
          </div>
          <Input
            v-model:value="form.name"
            :placeholder="$t('dhcp.subnets.form.namePlaceholder')"
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
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.lifetime') }}
          </div>
          <div class="flex items-center gap-2">
            <InputNumber
              v-model:value="form.validLifetime"
              :max="2592000"
              :min="60"
              style="width: 120px"
            />
            <Select
              :placeholder="$t('dhcp.subnets.form.presetPlaceholder')"
              style="width: 92px"
              :options="lifetimePresets"
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
        {{ $t('dhcp.subnets.form.poolSection') }}
        <span class="text-xs text-gray-400">
          {{ $t('dhcp.subnets.form.poolHint') }}
        </span>
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <template v-if="isV6">
          <div>
            <div class="mb-1 text-xs text-gray-400">
              {{ $t('dhcp.subnets.form.poolKind') }}
            </div>
            <Select
              v-model:value="form.poolKind"
              style="width: 120px"
              :options="poolKindOptions"
            />
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">
              {{
                form.poolKind === 'pd'
                  ? $t('dhcp.subnets.form.delegatedPrefix')
                  : $t('dhcp.subnets.form.poolStart')
              }}
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
              <div class="mb-1 text-xs text-gray-400">
                {{ $t('dhcp.subnets.form.prefixLen') }}
              </div>
              <InputNumber
                v-model:value="form.poolPrefixLen"
                :max="128"
                :min="1"
                style="width: 90px"
              />
            </div>
            <div>
              <div class="mb-1 text-xs text-gray-400">
                {{ $t('dhcp.subnets.form.delegatedLen') }}
              </div>
              <InputNumber
                v-model:value="form.poolDelegatedLen"
                :max="128"
                :min="1"
                style="width: 90px"
              />
            </div>
          </template>
          <div v-else>
            <div class="mb-1 text-xs text-gray-400">
              {{ $t('dhcp.subnets.form.poolEnd') }}
            </div>
            <Input
              v-model:value="form.poolEnd"
              placeholder="2406:172::200"
              style="width: 170px"
            />
          </div>
        </template>
        <template v-else>
          <div>
            <div class="mb-1 text-xs text-gray-400">
              {{ $t('dhcp.subnets.form.poolStart') }}
            </div>
            <Input
              v-model:value="form.poolStart"
              placeholder="10.61.172.100"
              style="width: 170px"
            />
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">
              {{ $t('dhcp.subnets.form.poolEnd') }}
            </div>
            <Input
              v-model:value="form.poolEnd"
              placeholder="10.61.172.200"
              style="width: 170px"
            />
          </div>
        </template>
      </div>

      <!-- 规划信息（M6-022）：仅规划展示，不参与 Kea 下发 -->
      <Divider plain style="margin: 16px 0 12px" orientation="left">
        {{ $t('dhcp.subnets.form.planning') }}
        <span class="text-xs text-gray-400">
          {{ $t('dhcp.subnets.form.planningHint') }}
        </span>
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <div class="mb-1 text-xs text-gray-400">VLAN</div>
          <Input
            v-model:value="form.vlan"
            placeholder="100"
            style="width: 120px"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.site') }}
          </div>
          <Input
            v-model:value="form.site"
            :placeholder="$t('dhcp.subnets.form.sitePlaceholder')"
            style="width: 180px"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.purpose') }}
          </div>
          <Input
            v-model:value="form.purpose"
            :placeholder="$t('dhcp.subnets.form.purposePlaceholder')"
            style="width: 180px"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.owner') }}
          </div>
          <Input
            v-model:value="form.owner"
            :placeholder="$t('dhcp.subnets.form.ownerPlaceholder')"
            style="width: 180px"
          />
        </div>
      </div>

      <!-- 网关与 DNS -->
      <Divider plain style="margin: 16px 0 12px" orientation="left">
        {{ $t('dhcp.subnets.form.gatewaySection') }}
        <span class="text-xs text-gray-400">
          {{ $t('dhcp.subnets.form.gatewayHint') }}
        </span>
      </Divider>
      <div class="flex flex-wrap items-end gap-3">
        <div v-if="!isV6">
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.gateway') }}
          </div>
          <Input
            v-model:value="form.gateway"
            placeholder="10.61.40.1"
            style="width: 180px"
          />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">
            {{ $t('dhcp.subnets.form.dns') }}
          </div>
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
        {{ $t('dhcp.subnets.form.optionsSection') }}
        <span class="text-xs text-gray-400">
          {{ $t('dhcp.subnets.form.optionsHint') }}
        </span>
      </Divider>
      <div class="mb-2 flex items-center gap-2">
        <Button size="small" @click="addOptionRow">
          {{ $t('dhcp.subnets.form.addOption') }}
        </Button>
      </div>
      <div
        v-for="(o, i) in form.options"
        :key="i"
        class="mb-2 flex flex-wrap items-center gap-2"
      >
        <Select
          :placeholder="$t('dhcp.subnets.form.presetOptionsPlaceholder')"
          style="width: 240px"
          :options="optionPresetOptions"
          :value="o.code || undefined"
          @change="(v: unknown) => onPresetChange(i, Number(v))"
        />
        <span class="text-xs text-gray-400">
          {{ $t('dhcp.subnets.form.orCode') }}
        </span>
        <InputNumber
          style="width: 90px"
          :max="isV6 ? 65535 : 254"
          :min="1"
          :value="o.code"
          @change="(v: unknown) => setOptionCode(i, Number(v))"
        />
        <Input
          v-model:value="o.data"
          :placeholder="$t('dhcp.subnets.form.valuePlaceholder')"
          style="width: 240px"
        />
        <span class="text-xs text-gray-400">CSV</span>
        <Switch v-model:checked="o.csvFormat" size="small" />
        <Button danger size="small" @click="removeOptionRow(i)">
          {{ $t('dhcp.common.remove') }}
        </Button>
      </div>
      <div v-if="form.options.length === 0" class="text-xs text-gray-400">
        {{ $t('dhcp.subnets.form.optionsEmpty') }}
      </div>
    </FormModal>
  </div>
</template>
