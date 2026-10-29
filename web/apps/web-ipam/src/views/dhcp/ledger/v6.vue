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

const showTotal = (t: number) => `共 ${t} 条`;

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
const kindOptions = [
  { label: '全部类型', value: 'all' },
  { label: '地址池', value: 'dynamic' },
  { label: 'PD 委派', value: 'pd' },
];

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
const expiryOptions = [
  { label: '全部到期状态', value: 'all' },
  { label: '已过期', value: 'expired' },
  { label: '24 小时内到期', value: '24h' },
  { label: '7 天内到期', value: '7d' },
];

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
  if (mins < 60) return `${mins} 分钟`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `${hours} 小时`;
  return `${Math.round(hours / 24)} 天`;
}

function expiryText(row: LedgerRow): string {
  const ms = remainingMs(row);
  if (ms === null) return '无租期';
  return ms < 0 ? `已过期 ${fmtDuration(ms)}` : `剩余 ${fmtDuration(ms)}`;
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

// ── 展示辅助 ──
const v6Cols = [
  { title: '网段（CIDR / 名称）', dataIndex: 'cidr', minWidth: 240 },
  { title: '类型', dataIndex: 'kind', width: 100 },
  { title: '地址池 / PD 委派', dataIndex: 'pools', minWidth: 280 },
  { title: '在线', dataIndex: 'online', width: 90 },
];

function poolText(p: Subnet): string {
  return (
    (p.pools ?? [])
      .map((x: any) =>
        x.kind === 'pd'
          ? `PD ${x.startAddr}/${x.prefixLen} → /${x.delegatedLen}`
          : `${x.startAddr} — ${x.endAddr ?? ''}`,
      )
      .join('；') || '未配置池'
  );
}
function kindText(p: Subnet): string {
  const kinds = new Set((p.pools ?? []).map((x: any) => x.kind));
  if (kinds.has('pd')) return 'PD 委派';
  if (kinds.has('dynamic')) return '地址池';
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
      title="IPv6 地址台账"
      description="按子网汇总 IPv6 地址池与 PD 委派使用情况，支持检索在线租约、查看到期剩余时间；IPv6 以子网级汇总，不提供逐地址矩阵。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:globe" class="size-3.5" />
          {{ v6Subnets.length }} 个 IPv6 网段 · 在线 {{ onlineTotal }}
        </span>
        <Button size="small" :loading="onlineLoading" @click="loadOnline">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          刷新
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
          <div class="ipam-empty__title">暂无 IPv6 网段</div>
          <div class="ipam-empty__desc">请先在左侧选择组织，或前往「子网管理」创建 IPv6 网段。</div>
        </div>

        <template v-else>
          <div class="ipam-stat-grid">
            <StatCard
              label="IPv6 网段"
              :value="v6Subnets.length"
              unit="个"
              icon="lucide:globe"
              tone="violet"
              :hint="`${activeSubnetCount} 个网段有在线地址`"
            />
            <StatCard
              label="在线地址"
              :value="onlineTotal"
              unit="条"
              icon="lucide:radio"
              tone="green"
              :hint="`覆盖 ${activeSubnetCount} 个网段 · 含 NA/PD 租约`"
            />
            <StatCard
              label="PD 委派池"
              :value="pdPoolCount"
              unit="个"
              icon="lucide:git-branch"
              tone="cyan"
              :hint="`动态地址池 ${dynamicPoolCount} 个`"
            />
            <StatCard
              label="当前网段在线"
              :value="onlineRows.length"
              unit="条"
              icon="lucide:activity"
              tone="blue"
              :hint="selectedCidr || '未选择网段'"
            />
          </div>

          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:layers" />
                IPv6 子网概览
                <em class="ipam-card-sub">{{ filteredSubnets.length }} / {{ v6Subnets.length }} 个</em>
              </span>
            </template>
            <template #extra>
              <Space :size="6" wrap>
                <Tag color="geekblue" class="m-0">{{ v6Subnets.length }} 个网段</Tag>
                <Tag color="green" class="m-0">{{ onlineTotal }} 在线</Tag>
                <span class="ipam-hint">地址由 PD 委派 / 地址池动态分配</span>
              </Space>
            </template>

            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">关键字</span>
                <Input
                  v-model:value="subnetKeyword"
                  allow-clear
                  placeholder="CIDR / 子网名称"
                  style="width: 240px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">类型</span>
                <Select v-model:value="subnetKind" :options="kindOptions" style="width: 140px" />
              </div>
              <Button size="small" @click="resetSubnetFilters">重置</Button>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">点击表格行切换下方在线租约</span>
            </div>

            <Table
              :key="selectedOrgId"
              :data-source="filteredSubnets"
              :columns="v6Cols"
              :locale="{ emptyText: subnetKeyword || subnetKind !== 'all' ? '没有匹配的 IPv6 网段' : '当前组织暂无 IPv6 网段' }"
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
                    <span class="ipam-hint">{{ (record as Subnet).name || '未命名网段' }}</span>
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
                在线租约
                <em class="ipam-card-sub">
                  {{ selectedCidr || '未选择网段' }} · {{ filteredOnlineRows.length }} /
                  {{ onlineRows.length }} 条
                </em>
              </span>
            </template>
            <template #extra>
              <Space :size="6" wrap>
                <Tag v-if="currentSubnet?.dnsServers" class="m-0">DNS {{ currentSubnet.dnsServers }}</Tag>
                <span class="ipam-hint">客户端标识为 DHCPv6 DUID</span>
              </Space>
            </template>

            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">关键字</span>
                <Input
                  v-model:value="onlineKeyword"
                  allow-clear
                  placeholder="地址 / DUID / 主机名 / 使用人"
                  style="width: 260px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">到期</span>
                <Select v-model:value="expiryFilter" :options="expiryOptions" style="width: 150px" />
              </div>
              <Button size="small" @click="resetOnlineFilters">重置</Button>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">点击上方网段行切换</span>
            </div>

            <Table
              :key="selectedCidr"
              :data-source="filteredOnlineRows"
              :loading="onlineLoading"
              :columns="[
                { title: '在线地址 / 前缀', dataIndex: 'address', minWidth: 250 },
                { title: '主机名', dataIndex: 'hostname', minWidth: 160 },
                { title: '使用人', dataIndex: 'owner', width: 110 },
                { title: '到期时间', dataIndex: 'leaseExpiry', width: 210 },
              ]"
              :locale="{ emptyText: onlineKeyword || expiryFilter !== 'all' ? '没有匹配的在线租约' : '该网段暂无在线地址' }"
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
