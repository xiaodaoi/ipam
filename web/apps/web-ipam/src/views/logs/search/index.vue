<script setup lang="ts">
import type { Dayjs } from 'dayjs';

import type { LogQuery, LogRow, QpsPoint, TopEntry } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { useVbenDrawer } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  DatePicker,
  Input,
  message,
  Select,
  Table,
  TabPane,
  Tabs,
  Tag,
} from 'ant-design-vue';

import { exportLogsCsv, getLogQps, listLogs, listLogTop } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';
import { $t } from '#/locales';

import { useLogWindowStats } from '../use-log-window-stats';

const STORAGE_KEY = 'ipam.logs.search.filters.v1';

const PRESET_HOURS = [
  { hours: 1, key: '1h' },
  { hours: 6, key: '6h' },
  { hours: 24, key: '24h' },
  { hours: 168, key: '7d' },
] as const;
type PresetKey = 'custom' | (typeof PRESET_HOURS)[number]['key'];

const presets = computed(() =>
  PRESET_HOURS.map((p) => ({
    ...p,
    label: $t(`logs.common.range.${p.key}`),
  })),
);

const ACTION_OPTIONS = computed(() => [
  { label: $t('logs.search.actionOptions.resolve'), value: 'resolve' },
  { label: $t('logs.search.actionOptions.blocked'), value: 'blocked' },
  { label: $t('logs.search.actionOptions.passthru'), value: 'passthru' },
  { label: $t('logs.search.actionOptions.cached'), value: 'cached' },
  { label: $t('logs.search.actionOptions.lease_commit'), value: 'lease_commit' },
  { label: $t('logs.search.actionOptions.lease_renew'), value: 'lease_renew' },
  { label: $t('logs.search.actionOptions.lease_release'), value: 'lease_release' },
  { label: $t('logs.search.actionOptions.lease_expire'), value: 'lease_expire' },
  { label: $t('logs.search.actionOptions.lease_decline'), value: 'lease_decline' },
]);

const TYPE_META: Record<string, { color: string; label: string }> = {
  dhcp: { color: 'cyan', label: 'DHCP' },
  dns: { color: 'blue', label: 'DNS' },
};
const ACTION_COLOR: Record<string, string> = {
  blocked: 'red',
  dns_query: 'blue',
  resolve: 'green',
  passthru: 'default',
  cached: 'geekblue',
  lease_commit: 'cyan',
  lease_renew: 'blue',
  lease_release: 'orange',
  lease_expire: 'volcano',
  lease_decline: 'red',
};
const RCODE_COLOR: Record<string, string> = {
  NOERROR: 'green',
  NXDOMAIN: 'orange',
  NODATA: 'gold',
  SERVFAIL: 'red',
  REFUSED: 'red',
};

const router = useRouter();
const route = useRoute();

function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString('zh-CN');
}
function parseTs(v?: string): Date | undefined {
  if (!v) return undefined;
  const t = Date.parse(v);
  return Number.isNaN(t) || t <= 0 ? undefined : new Date(t);
}
function fmtDate(v?: string): string {
  return parseTs(v)?.toLocaleDateString('zh-CN') ?? '—';
}
function fmtTime(v?: string, ms = false): string {
  const d = parseTs(v);
  if (!d) return '—';
  return d.toLocaleTimeString('zh-CN', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    ...(ms
      ? { fractionalSecondDigits: 3 as const }
      : {}),
  });
}
function fmtDateTime(v?: string): string {
  const d = parseTs(v);
  return d ? `${fmtDate(v)} ${fmtTime(v)}` : '—';
}
function fmtMac(mac?: string): string {
  if (!mac) return '';
  const hex = mac.replaceAll(/[^0-9a-f]/gi, '').toLowerCase();
  return hex.length === 12
    ? (hex.match(/.{2}/g) ?? []).join(':')
    : mac;
}
function pct(part: number, all: number): string {
  if (!all) return '0%';
  return `${((part / all) * 100).toFixed(1)}%`;
}

// ── 时间窗（快照语义：查询时冻结，避免翻页过程中时间漂移）──
const preset = ref<PresetKey>('6h');
const customRange = ref<[Dayjs, Dayjs]>();
const window = ref({ from: '', to: '', labelKey: 'logs.common.range.6h' });
const windowLabel = computed(() => $t(window.value.labelKey));

function refreshWindow(): boolean {
  if (preset.value === 'custom') {
    if (!customRange.value) {
      message.info($t('logs.search.messages.selectRange'));
      return false;
    }
    window.value = {
      from: customRange.value[0].toISOString(),
      to: customRange.value[1].toISOString(),
      labelKey: 'logs.common.customRange',
    };
    return true;
  }
  const p = PRESET_HOURS.find((x) => x.key === preset.value) ?? PRESET_HOURS[1]!;
  window.value = {
    from: new Date(Date.now() - p.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
    labelKey: `logs.common.range.${p.key}`,
  };
  return true;
}

// ── 过滤条件 ──
const filters = reactive({
  action: undefined as string | undefined,
  answerIp: '',
  domain: '',
  ip: '',
  mac: '',
  q: '',
  sip: '',
  type: undefined as string | undefined,
});
const advanced = ref(false);

function filterParts(): Omit<LogQuery, 'from'> {
  const q: Omit<LogQuery, 'from'> = {};
  if (filters.type) q.type = filters.type;
  if (filters.action) q.action = filters.action;
  if (filters.domain.trim()) q.domain = filters.domain.trim();
  if (filters.mac.trim()) q.mac = filters.mac.trim();
  if (filters.ip.trim()) q.ip = filters.ip.trim();
  if (filters.sip.trim()) q.sip = filters.sip.trim();
  if (filters.answerIp.trim()) q.answerIp = filters.answerIp.trim();
  if (filters.q.trim()) q.q = filters.q.trim();
  return q;
}

const activeFilterCount = computed(() => Object.keys(filterParts()).length);

function saveFilters() {
  try {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ filters, preset: preset.value }),
    );
  } catch {
    /* 隐私模式/配额不足时静默降级 */
  }
}

function restoreFilters() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return;
    const saved = JSON.parse(raw) as {
      filters?: Partial<typeof filters>;
      preset?: string;
    };
    if (saved.filters) {
      Object.assign(filters, saved.filters);
    }
    if (saved.preset && PRESET_HOURS.some((p) => p.key === saved.preset)) {
      preset.value = saved.preset as PresetKey;
    }
  } catch {
    /* 损坏的缓存直接忽略 */
  }
}

function applyRouteQuery() {
  const q = route.query;
  const str = (v: unknown) =>
    typeof v === 'string' && v.trim() ? v.trim() : '';
  if (['dhcp', 'dns'].includes(str(q.type))) filters.type = str(q.type);
  if (str(q.action)) filters.action = str(q.action);
  if (str(q.domain)) filters.domain = str(q.domain);
  if (str(q.mac)) filters.mac = str(q.mac);
  if (str(q.ip)) filters.ip = str(q.ip);
  if (str(q.sip)) filters.sip = str(q.sip);
  if (str(q.answerIp)) filters.answerIp = str(q.answerIp);
  if (str(q.q)) filters.q = str(q.q);
  const range = str(q.range);
  if (PRESET_HOURS.some((p) => p.key === range)) preset.value = range as PresetKey;
}

function syncRoute() {
  const query: Record<string, string> = { range: preset.value };
  for (const [k, v] of Object.entries(filterParts())) {
    if (typeof v === 'string' && v) query[k] = v;
  }
  void router.replace({ query });
}

// ── 明细查询 ──
const rows = ref<LogRow[]>([]);
const total = ref(0);
const loading = ref(false);
const page = ref(1);
const pageSize = ref(50);
// 深分页护栏（M6-015）：后端最多允许 offset 1 万，前端同步收敛页码并提示。
const MAX_OFFSET = 10_000;
const reachableTotal = computed(() =>
  Math.min(total.value, Math.floor(MAX_OFFSET / pageSize.value) * pageSize.value),
);
const deepPagingLimited = computed(() => total.value > reachableTotal.value);

async function fetchRows() {
  loading.value = true;
  try {
    const p = await listLogs({
      from: window.value.from,
      to: window.value.to,
      ...filterParts(),
      page: page.value,
      pageSize: pageSize.value,
    });
    rows.value = p.items ?? [];
    total.value = p.total ?? rows.value.length;
  } catch (error) {
    rows.value = [];
    total.value = 0;
    message.error(
      error instanceof Error ? error.message : $t('logs.search.messages.queryFail'),
    );
  } finally {
    loading.value = false;
  }
}

function onPageChange(p: number, size: number) {
  page.value = p;
  pageSize.value = size;
  void fetchRows();
}

// ── 时间窗 KPI ──
const kpi = useLogWindowStats();

// ── 聚合（TopN / QPS）──
const topDomains = ref<TopEntry[]>([]);
const topClients = ref<TopEntry[]>([]);
const qpsPoints = ref<QpsPoint[]>([]);
const aggLoading = ref(false);
const activeTab = ref('rows');

const qpsChart = computed(() =>
  qpsPoints.value.map((p) => ({ ts: p.ts, value: p.count })),
);
const qpsTotal = computed(() =>
  qpsPoints.value.reduce((sum, p) => sum + p.count, 0),
);
const qpsPeak = computed(() =>
  Math.max(0, ...qpsPoints.value.map((p) => p.count)),
);
const qpsAvg = computed(() =>
  qpsPoints.value.length > 0 ? qpsTotal.value / qpsPoints.value.length : 0,
);
const topDomainMax = computed(() =>
  Math.max(1, ...topDomains.value.map((d) => d.count)),
);
const topClientMax = computed(() =>
  Math.max(1, ...topClients.value.map((d) => d.count)),
);

function aggBucketSeconds(): number {
  const start = Date.parse(window.value.from);
  const end = Date.parse(window.value.to);
  return Math.max(60, Math.round((end - start) / 1000 / 48));
}

async function loadAgg() {
  aggLoading.value = true;
  try {
    const base: LogQuery = { from: window.value.from, to: window.value.to };
    if (filters.type) base.type = filters.type;
    if (filters.action) base.action = filters.action;
    const [domains, clients, qps] = await Promise.all([
      listLogTop({ ...base, by: 'domain', limit: 100 }),
      listLogTop({ ...base, by: 'client', limit: 10 }),
      getLogQps({ ...base, intervalSec: aggBucketSeconds() }),
    ]);
    topDomains.value = domains.items ?? [];
    topClients.value = clients.items ?? [];
    qpsPoints.value = qps.points ?? [];
  } catch {
    topDomains.value = [];
    topClients.value = [];
    qpsPoints.value = [];
  } finally {
    aggLoading.value = false;
  }
}

// ── 查询入口 ──
function query() {
  if (!refreshWindow()) return;
  page.value = 1;
  saveFilters();
  syncRoute();
  void fetchRows();
  void kpi.load({ from: window.value.from, to: window.value.to });
  void loadAgg();
}

function reload() {
  query();
}

function resetFilters() {
  filters.type = undefined;
  filters.action = undefined;
  filters.domain = '';
  filters.mac = '';
  filters.ip = '';
  filters.sip = '';
  filters.answerIp = '';
  filters.q = '';
  preset.value = '6h';
  customRange.value = undefined;
  query();
}

function onPreset(key: PresetKey) {
  preset.value = key;
  if (key !== 'custom') {
    customRange.value = undefined;
    query();
  }
}

function onRangeChange(v: unknown) {
  if (!v) {
    if (preset.value === 'custom') preset.value = '6h';
    return;
  }
  preset.value = 'custom';
  customRange.value = v as [Dayjs, Dayjs];
  query();
}

async function onExport() {
  try {
    const params: Record<string, string> = {
      from: window.value.from,
      to: window.value.to,
    };
    for (const [k, v] of Object.entries(filterParts())) {
      if (typeof v === 'string' && v) params[k] = v;
    }
    await exportLogsCsv(params);
    message.success($t('logs.search.messages.exportStarted'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('logs.search.messages.exportFail'),
    );
  }
}

// ── 明细抽屉 ──
const detail = ref<LogRow>();
const [DetailDrawer, detailDrawerApi] = useVbenDrawer({
  class: 'w-[620px]',
  footer: false,
  title: $t('logs.search.detail.title'),
});

function openDetail(r: LogRow) {
  detail.value = r;
  detailDrawerApi.open();
}

function rowProps(record: LogRow) {
  return {
    class: 'ipam-row-link',
    onClick: () => openDetail(record),
  };
}

async function copyDetail() {
  if (!detail.value) return;
  try {
    await navigator.clipboard.writeText(JSON.stringify(detail.value, null, 2));
    message.success($t('logs.search.detail.copied'));
  } catch {
    message.warning($t('logs.common.clipboardDenied'));
  }
}

const columns = computed(() => [
  { dataIndex: 'ts', key: 'ts', title: $t('logs.search.columns.time'), width: 132 },
  { dataIndex: 'type', key: 'type', title: $t('logs.search.columns.type'), width: 76 },
  { dataIndex: 'domain', ellipsis: true, key: 'domain', title: $t('logs.search.columns.domain') },
  { dataIndex: 'client', key: 'client', title: $t('logs.search.columns.client'), width: 178 },
  { dataIndex: 'answer', key: 'answer', title: $t('logs.search.columns.answer'), width: 170 },
  { dataIndex: 'sip', key: 'sip', title: $t('logs.search.columns.sip'), width: 132 },
  { dataIndex: 'action', key: 'action', title: $t('logs.search.columns.action'), width: 118 },
]);
const topDomainColumns = computed(() => [
  { dataIndex: 'rank', key: 'rank', title: '#', width: 52 },
  { dataIndex: 'key', key: 'key', title: $t('logs.search.aggColumns.domain'), ellipsis: true },
  { dataIndex: 'count', key: 'count', title: $t('logs.search.aggColumns.count'), width: 110 },
]);
const topClientColumns = computed(() => [
  { dataIndex: 'rank', key: 'rank', title: '#', width: 52 },
  { dataIndex: 'key', key: 'key', title: $t('logs.search.aggColumns.mac'), width: 200 },
  { dataIndex: 'count', key: 'count', title: $t('logs.search.aggColumns.count'), width: 110 },
]);

function withRank(list: TopEntry[]) {
  return list.map((item, i) => ({ ...item, rank: i + 1 }));
}
const topDomainRows = computed(() => withRank(topDomains.value));
const topClientRows = computed(() => withRank(topClients.value));

// 热门域名分页（M6-020）：拉取更多条目后按页展示，排名全局连续。
const domainPagination = computed(() => ({
  pageSize: 10,
  pageSizeOptions: ['10', '20', '50'],
  showSizeChanger: true,
  showQuickJumper: true,
  size: 'small' as const,
  showTotal: (total: number) => $t('logs.search.domainTotal', { n: total }),
}));

const kpiValue = (v?: number) => (v === undefined ? '—' : fmtInt(v));
const stats = computed(() => kpi.stats.value);

const resultHint = computed(() => {
  const base = $t('logs.search.results.hint', { n: pageSize.value });
  return deepPagingLimited.value
    ? base + $t('logs.search.results.deepPagingLimited')
    : base;
});
const rowsEmpty = computed(() =>
  activeFilterCount.value > 0 || preset.value !== '6h'
    ? $t('logs.search.results.emptyFiltered')
    : $t('logs.search.results.empty'),
);

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  restoreFilters();
  applyRouteQuery();
  if (!refreshWindow()) {
    preset.value = '6h';
    refreshWindow();
  }
  void fetchRows();
  void kpi.load({ from: window.value.from, to: window.value.to });
  void loadAgg();
  // 30s 仅刷新聚合（Top/QPS），不打断明细阅读
  timer = setInterval(() => void loadAgg(), 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:search"
      :title="$t('logs.search.title')"
      :description="$t('logs.search.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:database" class="size-3.5" />
          {{ $t('logs.search.hits', { range: windowLabel, n: fmtInt(total) }) }}
        </span>
        <Button size="small" @click="reload">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{ $t('logs.common.refresh') }}
        </Button>
        <Button size="small" type="primary" @click="onExport">
          <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />{{ $t('logs.common.exportCsv') }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('logs.search.kpi.total')"
        :value="kpiValue(stats?.total)"
        icon="lucide:activity"
        tone="blue"
        :spark="stats?.spark"
        :hint="$t('logs.search.kpi.totalHint', { range: windowLabel, n: stats?.buckets ?? 0 })"
      />
      <StatCard
        :label="$t('logs.search.kpi.dns')"
        :value="kpiValue(stats?.dns)"
        icon="lucide:globe"
        tone="cyan"
        :hint="$t('logs.common.share', { pct: pct(stats?.dns ?? 0, stats?.total ?? 0) })"
      />
      <StatCard
        :label="$t('logs.search.kpi.dhcp')"
        :value="kpiValue(stats?.dhcp)"
        icon="lucide:network"
        tone="green"
        :hint="$t('logs.common.share', { pct: pct(stats?.dhcp ?? 0, stats?.total ?? 0) })"
      />
      <StatCard
        :label="$t('logs.search.kpi.blocked')"
        :value="kpiValue(stats?.blocked)"
        icon="lucide:shield-ban"
        :tone="(stats?.blocked ?? 0) > 0 ? 'red' : 'violet'"
        :hint="(stats?.blocked ?? 0) > 0 ? $t('logs.common.blockedShare', { pct: pct(stats?.blocked ?? 0, stats?.dns ?? 0) }) : $t('logs.common.noBlocked')"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:sliders-horizontal" />
          {{ $t('logs.search.filters.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">
          {{ activeFilterCount > 0 ? $t('logs.search.filters.active', { n: activeFilterCount }) : $t('logs.search.filters.inactive') }}
        </span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-seg">
          <button
            v-for="p in presets"
            :key="p.key"
            type="button"
            :data-active="preset === p.key"
            @click="onPreset(p.key)"
          >
            {{ p.label }}
          </button>
          <button
            type="button"
            :data-active="preset === 'custom'"
            @click="onPreset('custom')"
          >
            {{ $t('logs.common.custom') }}
          </button>
        </div>
        <DatePicker.RangePicker
          :value="customRange"
          :allow-clear="true"
          :show-time="{ format: 'HH:mm:ss' }"
          format="YYYY-MM-DD HH:mm:ss"
          style="width: 340px"
          :placeholder="[$t('logs.common.startTime'), $t('logs.common.endTime')]"
          @change="onRangeChange"
        />
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="advanced = !advanced">
          <IconifyIcon icon="lucide:list-filter" class="mr-1 size-3.5" />
          {{ advanced ? $t('logs.search.filters.collapse') : $t('logs.search.filters.more') }}
        </Button>
        <Button size="small" @click="resetFilters">{{ $t('logs.common.reset') }}</Button>
        <Button type="primary" size="small" @click="query">
          <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />{{ $t('logs.common.query') }}
        </Button>
      </div>

      <div class="ipam-filter-grid">
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{ $t('logs.search.filters.keyword') }}</span>
          <Input
            v-model:value="filters.q"
            allow-clear
            :placeholder="$t('logs.search.filters.keywordPlaceholder')"
            @press-enter="query"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{ $t('logs.search.filters.type') }}</span>
          <Select
            v-model:value="filters.type"
            allow-clear
            :placeholder="$t('logs.search.filters.typePlaceholder')"
            :options="[
              { value: 'dns', label: $t('logs.search.filters.typeDns') },
              { value: 'dhcp', label: $t('logs.search.filters.typeDhcp') },
            ]"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{ $t('logs.search.filters.action') }}</span>
          <Select
            v-model:value="filters.action"
            allow-clear
            show-search
            option-filter-prop="label"
            :placeholder="$t('logs.search.filters.actionPlaceholder')"
            :options="ACTION_OPTIONS"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{ $t('logs.search.filters.domain') }}</span>
          <Input
            v-model:value="filters.domain"
            allow-clear
            :placeholder="$t('logs.search.filters.domainPlaceholder')"
            @press-enter="query"
          />
        </label>

        <template v-if="advanced">
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">{{ $t('logs.search.filters.mac') }}</span>
            <Input
              v-model:value="filters.mac"
              allow-clear
              :placeholder="$t('logs.search.filters.macPlaceholder')"
              @press-enter="query"
            />
          </label>
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">{{ $t('logs.search.filters.clientIp') }}</span>
            <Input
              v-model:value="filters.ip"
              allow-clear
              :placeholder="$t('logs.search.filters.clientIpPlaceholder')"
              @press-enter="query"
            />
          </label>
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">{{ $t('logs.search.filters.sip') }}</span>
            <Input
              v-model:value="filters.sip"
              allow-clear
              :placeholder="$t('logs.search.filters.sipPlaceholder')"
              @press-enter="query"
            />
          </label>
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">{{ $t('logs.search.filters.answerIp') }}</span>
            <Input
              v-model:value="filters.answerIp"
              allow-clear
              :placeholder="$t('logs.search.filters.answerIpPlaceholder')"
              @press-enter="query"
            />
          </label>
        </template>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:scroll-text" />
          {{ $t('logs.search.results.title') }}
          <em class="ipam-card-sub">{{ $t('logs.search.results.count', { n: fmtInt(total) }) }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ resultHint }}</span>
      </template>

      <Tabs v-model:active-key="activeTab">
        <TabPane key="rows" :tab="$t('logs.common.rowsDetail')">
          <Table
            :columns="columns"
            :custom-row="rowProps"
            :data-source="rows"
            :loading="loading"
            :pagination="{
              current: page,
              pageSize,
              total: reachableTotal,
              position: ['bottomLeft'],
              showSizeChanger: true,
              pageSizeOptions: ['20', '50', '100', '200'],
              showTotal: (t: number) => $t('logs.search.results.total', { n: t }),
              onChange: onPageChange,
            }"
            :row-key="(r: LogRow) => [r.ts, r.clientMac, r.domain, r.answerIp, r.action].join('|')"
            :scroll="{ x: 920 }"
            :locale="{ emptyText: rowsEmpty }"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'ts'">
                <span class="ipam-log-time">
                  <b>{{ fmtTime(record.ts) }}</b>
                  <span>{{ fmtDate(record.ts) }}</span>
                </span>
              </template>
              <template v-else-if="column.key === 'type'">
                <Tag class="m-0" :color="TYPE_META[record.type]?.color">
                  {{ TYPE_META[record.type]?.label ?? record.type }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'domain'">
                <span :title="record.domain" class="ipam-log-domain">
                  {{ record.domain || '—' }}
                </span>
              </template>
              <template v-else-if="column.key === 'client'">
                <span v-if="record.clientMac || record.clientIp" class="ipam-cell-stack">
                  <span v-if="record.clientMac" class="ipam-mono text-xs" :title="record.clientMac">
                    {{ fmtMac(record.clientMac) }}
                  </span>
                  <span v-if="record.clientIp" class="ipam-cell-meta ipam-mono" :title="record.clientIp">
                    {{ record.clientIp }}
                  </span>
                </span>
                <span v-else class="ipam-hint">—</span>
              </template>
              <template v-else-if="column.key === 'answer'">
                <span v-if="record.rcode || record.answerIp" class="ipam-cell-stack">
                  <Tag v-if="record.rcode" class="m-0" :color="RCODE_COLOR[record.rcode] ?? 'default'">
                    {{ record.rcode }}
                  </Tag>
                  <span v-if="record.answerIp" class="ipam-cell-meta ipam-mono" :title="record.answerIp">
                    {{ record.answerIp }}
                  </span>
                </span>
                <span v-else class="ipam-hint">—</span>
              </template>
              <template v-else-if="column.key === 'sip'">
                <span class="ipam-mono text-xs">{{ record.sip || '—' }}</span>
              </template>
              <template v-else-if="column.key === 'action'">
                <Tag class="m-0" :color="ACTION_COLOR[record.action] ?? 'default'">
                  {{ record.action || '—' }}
                </Tag>
              </template>
            </template>
          </Table>
        </TabPane>

        <TabPane key="domain" :tab="$t('logs.common.topDomains')">
          <Table
            :columns="topDomainColumns"
            :data-source="topDomainRows"
            :loading="aggLoading"
            :locale="{ emptyText: $t('logs.search.emptyDomain') }"
            :pagination="domainPagination"
            row-key="key"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'rank'">
                <span class="ipam-rank__no" :data-top="record.rank <= 3">{{ record.rank }}</span>
              </template>
              <template v-else-if="column.key === 'key'">
                <span :title="record.key" class="ipam-mono text-xs">{{ record.key }}</span>
                <div class="ipam-progress mt-1">
                  <i :style="{ width: `${Math.max(4, (record.count / topDomainMax) * 100)}%` }"></i>
                </div>
              </template>
              <template v-else-if="column.key === 'count'">{{ fmtInt(record.count) }}</template>
            </template>
          </Table>
        </TabPane>

        <TabPane key="client" :tab="$t('logs.common.topClients')">
          <Table
            :columns="topClientColumns"
            :data-source="topClientRows"
            :loading="aggLoading"
            :locale="{ emptyText: $t('logs.search.emptyClient') }"
            :pagination="false"
            row-key="key"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'rank'">
                <span class="ipam-rank__no" :data-top="record.rank <= 3">{{ record.rank }}</span>
              </template>
              <template v-else-if="column.key === 'key'">
                <span class="ipam-mono text-xs">{{ fmtMac(record.key) }}</span>
                <div class="ipam-progress mt-1">
                  <i :style="{ width: `${Math.max(4, (record.count / topClientMax) * 100)}%` }"></i>
                </div>
              </template>
              <template v-else-if="column.key === 'count'">{{ fmtInt(record.count) }}</template>
            </template>
          </Table>
        </TabPane>

        <TabPane key="qps" :tab="$t('logs.search.qps.tab')">
          <TrendChart
            v-if="qpsChart.length > 0"
            :data="qpsChart"
            :height="200"
            :markers="true"
            :time-format="preset === '1h' || preset === '6h' ? 'HH:mm' : 'MM-DD HH:mm'"
            tone="blue"
            :unit="$t('logs.common.times')"
          />
          <div v-else-if="!aggLoading" class="ipam-empty ipam-empty--compact">
            <div class="ipam-empty__title">{{ $t('logs.common.noTrend') }}</div>
            <div class="ipam-empty__desc">{{ $t('logs.search.qps.emptyDesc') }}</div>
          </div>
          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('logs.common.figures.total') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(qpsTotal) }}</span>
              <span class="ipam-figure__unit">{{ $t('logs.common.times') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('logs.common.figures.avg') }}</span>
              <span class="ipam-figure__value">{{ qpsAvg.toFixed(1) }}</span>
              <span class="ipam-figure__unit">{{ $t('logs.common.times') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('logs.common.figures.peak') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(qpsPeak) }}</span>
              <span class="ipam-figure__unit">{{ $t('logs.common.times') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('logs.common.figures.bucket') }}</span>
              <span class="ipam-figure__value">{{ aggBucketSeconds() }}</span>
              <span class="ipam-figure__unit">{{ $t('logs.common.seconds') }}</span>
            </div>
          </div>
        </TabPane>
      </Tabs>
    </Card>

    <DetailDrawer>
      <template v-if="detail">
        <div class="ipam-detail-head">
          <Tag class="m-0" :color="TYPE_META[detail.type]?.color">
            {{ TYPE_META[detail.type]?.label ?? detail.type }}
          </Tag>
          <Tag class="m-0" :color="ACTION_COLOR[detail.action ?? ''] ?? 'default'">
            {{ detail.action || 'unknown' }}
          </Tag>
          <span class="ipam-mono text-xs">{{ fmtDateTime(detail.ts) }}</span>
          <span class="ipam-toolbar__spacer"></span>
          <Button size="small" @click="copyDetail">
            <IconifyIcon icon="lucide:copy" class="mr-1 size-3.5" />{{ $t('logs.common.copyJson') }}
          </Button>
        </div>

        <div class="ipam-detail-grid">
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.time') }}</div>
            <div class="ipam-detail-item__value">{{ fmtDateTime(detail.ts) }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.severity') }}</div>
            <div class="ipam-detail-item__value">{{ detail.severity || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.domain') }}</div>
            <div class="ipam-detail-item__value">{{ detail.domain || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.action') }}</div>
            <div class="ipam-detail-item__value">{{ detail.action || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.mac') }}</div>
            <div class="ipam-detail-item__value ipam-mono">{{ fmtMac(detail.clientMac) || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.clientIp') }}</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.clientIp || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.sip') }}</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.sip || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.rcode') }}</div>
            <div class="ipam-detail-item__value">
              {{ detail.rcode || '—' }}
              <span v-if="detail.category" class="ipam-hint"> · {{ detail.category }}</span>
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.answerIp') }}</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.answerIp || '—' }}</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">{{ $t('logs.search.detail.raw') }}</div>
            <div class="ipam-detail-item__value">{{ detail.detail || '—' }}</div>
          </div>
        </div>
      </template>
    </DetailDrawer>
  </div>
</template>
