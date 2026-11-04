<script setup lang="ts">
import type { LedgerRow, OrgTreeNode, Subnet } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, Select, Space, Table, Tag } from 'ant-design-vue';

import { listLedger, listOrgTree, listSubnets } from '#/api/ipam';
import OrgFilterCard from '#/components/org-filter-card.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const showTotal = (t: number) => $t('dhcpLedger.common.showTotal', { total: t });

// ── 组织筛选 ──
const orgTree = ref<OrgTreeNode[]>([]);
const selectedOrgId = ref<string>('');

function onSelectOrg(orgId: string) {
  selectedOrgId.value = orgId;
  selectedCidr.value = '';
  void loadSubnets();
}

// ── IPv6 子网（台账视角）──
const subnets = ref<Subnet[]>([]);
const v6Subnets = computed(() => subnets.value.filter((s) => s.family === 6));
const selectedCidr = ref('');
const selectedSubnetId = computed(
  () => v6Subnets.value.find((s) => s.cidr === selectedCidr.value)?.id ?? '',
);
const currentSubnet = computed(() => v6Subnets.value.find((s) => s.cidr === selectedCidr.value));
const allOnline = ref<LedgerRow[]>([]);
const onlineCounts = ref<Record<string, number>>({});
const onlineLoading = ref(false);
const nowTs = ref(Date.now());
const onlineRows = computed(() =>
  allOnline.value.filter((r) => r.subnetId === selectedSubnetId.value),
);

// ── 汇总指标 ──
const onlineTotal = computed(() =>
  v6Subnets.value.reduce((acc, s) => acc + (onlineCounts.value[s.id] ?? 0), 0),
);
const activeSubnetCount = computed(
  () => v6Subnets.value.filter((s) => (onlineCounts.value[s.id] ?? 0) > 0).length,
);
const pdPoolCount = computed(() =>
  v6Subnets.value.reduce(
    (n, s) => n + (s.pools ?? []).filter((p) => p.kind === 'pd').length,
    0,
  ),
);
const dynamicPoolCount = computed(() =>
  v6Subnets.value.reduce(
    (n, s) => n + (s.pools ?? []).filter((p) => p.kind !== 'pd').length,
    0,
  ),
);

/** 拉全在线租约：游标翻页，避免大组织只统计首 500 条 */
async function fetchAllOnline(): Promise<LedgerRow[]> {
  const out: LedgerRow[] = [];
  let cursor: string | undefined;
  for (let i = 0; i < 20; i += 1) {
    const d = await listLedger({ family: 6, state: 'online', pageSize: 500, cursor });
    out.push(...(d.items ?? []));
    if (!d.nextCursor) break;
    cursor = d.nextCursor;
  }
  return out;
}

async function loadOnline() {
  onlineLoading.value = true;
  try {
    allOnline.value = await fetchAllOnline();
  } catch {
    allOnline.value = [];
  } finally {
    onlineLoading.value = false;
    nowTs.value = Date.now();
  }
  const cnt: Record<string, number> = {};
  for (const r of allOnline.value) {
    if (r.subnetId) cnt[r.subnetId] = (cnt[r.subnetId] ?? 0) + 1;
  }
  onlineCounts.value = cnt;
  // 未选中或选中网段已不在当前列表时自动回退：优先有在线地址的网段，否则取第一个
  const list = v6Subnets.value;
  if (list.length > 0 && !list.some((s) => s.cidr === selectedCidr.value)) {
    const hit = list.find((s) => (cnt[s.id] ?? 0) > 0);
    const pick = hit ?? list[0];
    if (pick) selectedCidr.value = pick.cidr;
  }
}

function onSelectSubnet(cidr: string) {
  selectedCidr.value = cidr;
}

async function loadSubnets() {
  const res = await listSubnets(selectedOrgId.value || undefined);
  subnets.value = res.items ?? [];
  await loadOnline();
}

// ── 子网筛选 ──
type KindFilter = 'all' | 'dynamic' | 'pd';
const subnetKeyword = ref('');
const subnetKind = ref<KindFilter>('all');
const kindOptions = computed(() => [
  { label: $t('dhcpLedger.v6.kindAll'), value: 'all' },
  { label: $t('dhcpLedger.v6.kindDynamic'), value: 'dynamic' },
  { label: $t('dhcpLedger.v6.kindPd'), value: 'pd' },
]);

const filteredSubnets = computed(() => {
  const kw = subnetKeyword.value.trim().toLowerCase();
  return v6Subnets.value.filter((s) => {
    if (kw && !`${s.cidr} ${s.name ?? ''}`.toLowerCase().includes(kw)) return false;
    const kinds = new Set((s.pools ?? []).map((p) => p.kind));
    if (subnetKind.value === 'pd') return kinds.has('pd');
    if (subnetKind.value === 'dynamic') return kinds.has('dynamic');
    return true;
  });
});

function resetSubnetFilters() {
  subnetKeyword.value = '';
  subnetKind.value = 'all';
}

// ── 在线租约筛选 ──
const onlineKeyword = ref('');
type ExpiryFilter = '7d' | '24h' | 'all' | 'expired';
const expiryFilter = ref<ExpiryFilter>('all');
const expiryOptions = computed(() => [
  { label: $t('dhcpLedger.expiryFilter.all'), value: 'all' },
  { label: $t('dhcpLedger.expiryFilter.expired'), value: 'expired' },
  { label: $t('dhcpLedger.expiryFilter.h24'), value: '24h' },
  { label: $t('dhcpLedger.expiryFilter.d7'), value: '7d' },
]);

function remainingMs(row: LedgerRow): null | number {
  if (!row.leaseExpiry) return null;
  const t = new Date(row.leaseExpiry).getTime();
  return Number.isNaN(t) ? null : t - nowTs.value;
}

function expiryTone(row: LedgerRow): string {
  const ms = remainingMs(row);
  if (ms === null) return 'muted';
  if (ms < 0) return 'red';
  if (ms < 86_400_000) return 'amber';
  if (ms < 7 * 86_400_000) return 'cyan';
  return 'green';
}

function fmtDuration(ms: number): string {
  const mins = Math.max(1, Math.round(Math.abs(ms) / 60_000));
  if (mins < 60) return $t('dhcpLedger.lease.minutes', { n: mins });
  const hours = Math.round(mins / 60);
  if (hours < 48) return $t('dhcpLedger.lease.hours', { n: hours });
  return $t('dhcpLedger.lease.days', { n: Math.round(hours / 24) });
}

function expiryText(row: LedgerRow): string {
  const ms = remainingMs(row);
  if (ms === null) return $t('dhcpLedger.lease.noLease');
  return ms < 0
    ? $t('dhcpLedger.lease.expired', { duration: fmtDuration(ms) })
    : $t('dhcpLedger.lease.remaining', { duration: fmtDuration(ms) });
}

const filteredOnlineRows = computed(() => {
  const kw = onlineKeyword.value.trim().toLowerCase();
  return onlineRows.value.filter((row) => {
    const ms = remainingMs(row);
    if (expiryFilter.value === 'expired' && (!(ms !== null && ms < 0))) return false;
    if (expiryFilter.value === '24h' && (!(ms !== null && ms >= 0 && ms <= 86_400_000))) return false;
    if (expiryFilter.value === '7d' && (!(ms !== null && ms >= 0 && ms <= 7 * 86_400_000))) return false;
    if (!kw) return true;
    return [row.address, row.mac, row.hostname, row.owner].some((v) =>
      String(v ?? '').toLowerCase().includes(kw),
    );
  });
});

function resetOnlineFilters() {
  onlineKeyword.value = '';
  expiryFilter.value = 'all';
}

const onlineColumns = computed(() => [
  { title: $t('dhcpLedger.v6.colAddress'), dataIndex: 'address', minWidth: 250 },
  { title: $t('dhcpLedger.v6.colHostname'), dataIndex: 'hostname', minWidth: 160 },
  { title: $t('dhcpLedger.v6.colOwner'), dataIndex: 'owner', width: 110 },
  { title: $t('dhcpLedger.v6.colExpiry'), dataIndex: 'leaseExpiry', width: 210 },
]);

const subnetEmptyText = computed(() =>
  subnetKeyword.value || subnetKind.value !== 'all'
    ? $t('dhcpLedger.v6.emptyFiltered')
    : $t('dhcpLedger.v6.emptyOrg'),
);

const onlineEmptyText = computed(() =>
  onlineKeyword.value || expiryFilter.value !== 'all'
    ? $t('dhcpLedger.v6.emptyFilteredOnline')
    : $t('dhcpLedger.v6.emptyOnline'),
);

// ── 展示辅助 ──
const v6Cols = computed(() => [
  { title: $t('dhcpLedger.v6.colCidrName'), dataIndex: 'cidr', minWidth: 240 },
  { title: $t('dhcpLedger.v6.colKind'), dataIndex: 'kind', width: 100 },
  { title: $t('dhcpLedger.v6.colPools'), dataIndex: 'pools', minWidth: 280 },
  { title: $t('dhcpLedger.v6.colOnline'), dataIndex: 'online', width: 90 },
]);

function poolText(p: Subnet): string {
  return (
    (p.pools ?? [])
      .map((x: any) =>
        x.kind === 'pd'
          ? `PD ${x.startAddr}/${x.prefixLen} → /${x.delegatedLen}`
          : `${x.startAddr} — ${x.endAddr ?? ''}`,
      )
      .join('；') || $t('dhcpLedger.v6.noPool')
  );
}
function kindText(p: Subnet): string {
  const kinds = new Set((p.pools ?? []).map((x: any) => x.kind));
  if (kinds.has('pd')) return $t('dhcpLedger.v6.kindPd');
  if (kinds.has('dynamic')) return $t('dhcpLedger.v6.kindDynamic');
  return '—';
}
function kindColor(p: Subnet): string {
  const kinds = new Set((p.pools ?? []).map((x: any) => x.kind));
  if (kinds.has('pd')) return 'purple';
  if (kinds.has('dynamic')) return 'geekblue';
  return 'default';
}

function fmtTime(v?: string): string {
  return v ? new Date(v).toLocaleString() : '—';
}

const route = useRoute();

onMounted(async () => {
  orgTree.value = await listOrgTree();
  // 支持子网管理页 CIDR 点击跳转（?cidr=2406:440:3c16:4006::/64）
  const want = route.query.cidr;
  if (want) {
    selectedCidr.value = String(want);
  }
  await loadSubnets();
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:network"
      :title="$t('dhcpLedger.v6.title')"
      :description="$t('dhcpLedger.v6.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:globe" class="size-3.5" />
          {{ $t('dhcpLedger.v6.meta', { count: v6Subnets.length, online: onlineTotal }) }}
        </span>
        <Button size="small" :loading="onlineLoading" @click="loadOnline">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          {{ $t('dhcpLedger.common.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-subnets-layout">
      <OrgFilterCard
        :org-tree="orgTree"
        :selected-org-id="selectedOrgId"
        @select="onSelectOrg"
      />

      <div class="ipam-subnets-main">
        <div v-if="v6Subnets.length === 0" class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:globe" class="size-5" />
          </div>
          <div class="ipam-empty__title">{{ $t('dhcpLedger.v6.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('dhcpLedger.v6.emptyDesc') }}</div>
        </div>

        <template v-else>
          <div class="ipam-stat-grid">
            <StatCard
              :label="$t('dhcpLedger.v6.statSubnets')"
              :value="v6Subnets.length"
              :unit="$t('dhcpLedger.common.units.count')"
              icon="lucide:globe"
              tone="violet"
              :hint="$t('dhcpLedger.v6.statSubnetsHint', { count: activeSubnetCount })"
            />
            <StatCard
              :label="$t('dhcpLedger.v6.statOnline')"
              :value="onlineTotal"
              :unit="$t('dhcpLedger.common.units.item')"
              icon="lucide:radio"
              tone="green"
              :hint="$t('dhcpLedger.v6.statOnlineHint', { count: activeSubnetCount })"
            />
            <StatCard
              :label="$t('dhcpLedger.v6.statPd')"
              :value="pdPoolCount"
              :unit="$t('dhcpLedger.common.units.count')"
              icon="lucide:git-branch"
              tone="cyan"
              :hint="$t('dhcpLedger.v6.statPdHint', { count: dynamicPoolCount })"
            />
            <StatCard
              :label="$t('dhcpLedger.v6.statCurrentOnline')"
              :value="onlineRows.length"
              :unit="$t('dhcpLedger.common.units.item')"
              icon="lucide:activity"
              tone="blue"
              :hint="selectedCidr || $t('dhcpLedger.common.noSubnetSelected')"
            />
          </div>

          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:layers" />
                {{ $t('dhcpLedger.v6.overviewCard') }}
                <em class="ipam-card-sub">
                  {{ $t('dhcpLedger.v6.overviewSub', { shown: filteredSubnets.length, total: v6Subnets.length }) }}
                </em>
              </span>
            </template>
            <template #extra>
              <Space :size="6" wrap>
                <Tag color="geekblue" class="m-0">{{ $t('dhcpLedger.v6.tagSubnets', { count: v6Subnets.length }) }}</Tag>
                <Tag color="green" class="m-0">{{ $t('dhcpLedger.v6.tagOnline', { online: onlineTotal }) }}</Tag>
                <span class="ipam-hint">{{ $t('dhcpLedger.v6.poolHint') }}</span>
              </Space>
            </template>

            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">{{ $t('dhcpLedger.common.keyword') }}</span>
                <Input
                  v-model:value="subnetKeyword"
                  allow-clear
                  :placeholder="$t('dhcpLedger.v6.keywordPlaceholder')"
                  style="width: 240px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">{{ $t('dhcpLedger.common.type') }}</span>
                <Select v-model:value="subnetKind" :options="kindOptions" style="width: 140px" />
              </div>
              <Button size="small" @click="resetSubnetFilters">{{ $t('dhcpLedger.common.reset') }}</Button>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">{{ $t('dhcpLedger.v6.rowHint') }}</span>
            </div>

            <Table
              :key="selectedOrgId"
              :data-source="filteredSubnets"
              :columns="v6Cols"
              :locale="{ emptyText: subnetEmptyText }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              :row-class-name="(r: any) => (r.cidr === selectedCidr ? 'ipam-row-active' : '')"
              :custom-row="(r: any) => ({ onClick: () => onSelectSubnet(r.cidr), style: { cursor: 'pointer' } })"
              row-key="id"
              size="middle"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'cidr'">
                  <div class="ipam-cell-stack">
                    <span class="ipam-mono">{{ record.cidr }}</span>
                    <span class="ipam-hint">{{ (record as Subnet).name || $t('dhcpLedger.common.unnamedSubnet') }}</span>
                  </div>
                </template>
                <template v-else-if="column.dataIndex === 'kind'">
                  <Tag :color="kindColor(record as Subnet)" class="m-0">{{ kindText(record as Subnet) }}</Tag>
                </template>
                <template v-else-if="column.dataIndex === 'pools'">
                  <span class="ipam-mono text-xs">{{ poolText(record as Subnet) }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'online'">
                  <Tag :color="(onlineCounts[(record as Subnet).id] ?? 0) > 0 ? 'green' : 'default'" class="m-0">
                    {{ onlineCounts[(record as Subnet).id] ?? 0 }}
                  </Tag>
                </template>
              </template>
            </Table>
          </Card>

          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:radio-tower" />
                {{ $t('dhcpLedger.leaseCard.title') }}
                <em class="ipam-card-sub">
                  {{
                    $t('dhcpLedger.leaseCard.sub', {
                      cidr: selectedCidr || $t('dhcpLedger.common.noSubnetSelected'),
                      shown: filteredOnlineRows.length,
                      total: onlineRows.length,
                    })
                  }}
                </em>
              </span>
            </template>
            <template #extra>
              <Space :size="6" wrap>
                <Tag v-if="currentSubnet?.dnsServers" class="m-0">DNS {{ currentSubnet.dnsServers }}</Tag>
                <span class="ipam-hint">{{ $t('dhcpLedger.v6.leaseCardHint') }}</span>
              </Space>
            </template>

            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">{{ $t('dhcpLedger.common.keyword') }}</span>
                <Input
                  v-model:value="onlineKeyword"
                  allow-clear
                  :placeholder="$t('dhcpLedger.v6.onlineKeywordPlaceholder')"
                  style="width: 260px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">{{ $t('dhcpLedger.common.expiry') }}</span>
                <Select v-model:value="expiryFilter" :options="expiryOptions" style="width: 150px" />
              </div>
              <Button size="small" @click="resetOnlineFilters">{{ $t('dhcpLedger.common.reset') }}</Button>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">{{ $t('dhcpLedger.v6.onlineRowHint') }}</span>
            </div>

            <Table
              :key="selectedCidr"
              :data-source="filteredOnlineRows"
              :loading="onlineLoading"
              :columns="onlineColumns"
              :locale="{ emptyText: onlineEmptyText }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              row-key="address"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'address'">
                  <div class="ipam-cell-stack">
                    <span class="ipam-mono">{{ (record as any).address }}</span>
                    <span class="ipam-cell-meta" :title="(record as any).mac">
                      DUID {{ (record as any).mac || '—' }}
                    </span>
                  </div>
                </template>
                <template v-else-if="column.dataIndex === 'hostname'">{{ (record as any).hostname || '—' }}</template>
                <template v-else-if="column.dataIndex === 'owner'">{{ (record as any).owner || '—' }}</template>
                <template v-else-if="column.dataIndex === 'leaseExpiry'">
                  <div class="ipam-cell-stack">
                    <span class="ipam-hint">{{ fmtTime((record as any).leaseExpiry) }}</span>
                    <span class="ipam-chip" :data-tone="expiryTone(record as any)">{{ expiryText(record as any) }}</span>
                  </div>
                </template>
              </template>
            </Table>
          </Card>
        </template>
      </div>
    </div>
  </div>
</template>
