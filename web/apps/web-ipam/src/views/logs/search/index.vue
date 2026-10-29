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

import { useLogWindowStats } from '../use-log-window-stats';

const STORAGE_KEY = 'ipam.logs.search.filters.v1';

const PRESETS = [
  { hours: 1, key: '1h', label: '近 1 小时' },
  { hours: 6, key: '6h', label: '近 6 小时' },
  { hours: 24, key: '24h', label: '近 24 小时' },
  { hours: 168, key: '7d', label: '近 7 天' },
] as const;
type PresetKey = 'custom' | (typeof PRESETS)[number]['key'];

const ACTION_OPTIONS = [
  { label: 'resolve（正常解析）', value: 'resolve' },
  { label: 'blocked（封禁拦截）', value: 'blocked' },
  { label: 'passthru（放行）', value: 'passthru' },
  { label: 'cached（缓存命中）', value: 'cached' },
  { label: 'lease_commit（租约下发）', value: 'lease_commit' },
  { label: 'lease_renew（租约续约）', value: 'lease_renew' },
  { label: 'lease_release（租约释放）', value: 'lease_release' },
  { label: 'lease_expire（租约过期）', value: 'lease_expire' },
  { label: 'lease_decline（客户端拒绝）', value: 'lease_decline' },
];

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
const window = ref({ from: '', to: '', label: '' });

function refreshWindow(): boolean {
  if (preset.value === 'custom') {
    if (!customRange.value) {
      message.info('请选择自定义时间范围');
      return false;
    }
    window.value = {
      from: customRange.value[0].toISOString(),
      to: customRange.value[1].toISOString(),
      label: '自定义区间',
    };
    return true;
  }
  const p = PRESETS.find((x) => x.key === preset.value) ?? PRESETS[1]!;
  window.value = {
    from: new Date(Date.now() - p.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
    label: p.label,
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
    if (saved.preset && PRESETS.some((p) => p.key === saved.preset)) {
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
  if (PRESETS.some((p) => p.key === range)) preset.value = range as PresetKey;
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
    message.error(error instanceof Error ? error.message : '日志查询失败');
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
      listLogTop({ ...base, by: 'domain', limit: 10 }),
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
    message.success('CSV 导出已开始下载');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导出失败');
  }
}

// ── 明细抽屉 ──
const detail = ref<LogRow>();
const [DetailDrawer, detailDrawerApi] = useVbenDrawer({
  class: 'w-[620px]',
  footer: false,
  title: '日志详情',
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
    message.success('已复制当前日志 JSON');
  } catch {
    message.warning('浏览器未授予剪贴板权限');
  }
}

const columns = [
  { dataIndex: 'ts', key: 'ts', title: '时间', width: 132 },
  { dataIndex: 'type', key: 'type', title: '类型', width: 76 },
  { dataIndex: 'domain', ellipsis: true, key: 'domain', title: '域名' },
  { dataIndex: 'client', key: 'client', title: '客户端', width: 178 },
  { dataIndex: 'answer', key: 'answer', title: '应答', width: 170 },
  { dataIndex: 'sip', key: 'sip', title: '来源 IP', width: 132 },
  { dataIndex: 'action', key: 'action', title: '动作', width: 118 },
];
const topDomainColumns = [
  { dataIndex: 'rank', key: 'rank', title: '#', width: 52 },
  { dataIndex: 'key', key: 'key', title: '域名', ellipsis: true },
  { dataIndex: 'count', key: 'count', title: '事件数', width: 110 },
];
const topClientColumns = [
  { dataIndex: 'rank', key: 'rank', title: '#', width: 52 },
  { dataIndex: 'key', key: 'key', title: '客户端 MAC', width: 200 },
  { dataIndex: 'count', key: 'count', title: '事件数', width: 110 },
];

function withRank(list: TopEntry[]) {
  return list.map((item, i) => ({ ...item, rank: i + 1 }));
}
const topDomainRows = computed(() => withRank(topDomains.value));
const topClientRows = computed(() => withRank(topClients.value));

const kpiValue = (v?: number) => (v === undefined ? '—' : fmtInt(v));
const stats = computed(() => kpi.stats.value);

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
      title="查询检索"
      description="跨 DHCP 与 DNS 统一检索：时间窗、事件类型、域名、客户端与应答地址可组合过滤，结果支持明细下钻与 CSV 导出。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:database" class="size-3.5" />
          {{ window.label }} · 命中 {{ fmtInt(total) }} 条
        </span>
        <Button size="small" @click="reload">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button size="small" type="primary" @click="onExport">
          <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />导出 CSV
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="时间窗事件总量"
        :value="kpiValue(stats?.total)"
        icon="lucide:activity"
        tone="blue"
        :spark="stats?.spark"
        :hint="`${window.label} · ${stats?.buckets ?? 0} 个时间分桶`"
      />
      <StatCard
        label="DNS 查询量"
        :value="kpiValue(stats?.dns)"
        icon="lucide:globe"
        tone="cyan"
        :hint="`占事件总量 ${pct(stats?.dns ?? 0, stats?.total ?? 0)}`"
      />
      <StatCard
        label="DHCP 事件"
        :value="kpiValue(stats?.dhcp)"
        icon="lucide:network"
        tone="green"
        :hint="`占事件总量 ${pct(stats?.dhcp ?? 0, stats?.total ?? 0)}`"
      />
      <StatCard
        label="封禁拦截"
        :value="kpiValue(stats?.blocked)"
        icon="lucide:shield-ban"
        :tone="(stats?.blocked ?? 0) > 0 ? 'red' : 'violet'"
        :hint="(stats?.blocked ?? 0) > 0 ? `占 DNS 查询 ${pct(stats?.blocked ?? 0, stats?.dns ?? 0)}` : '当前时间窗无拦截事件'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:sliders-horizontal" />
          检索条件
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">
          {{ activeFilterCount > 0 ? `已启用 ${activeFilterCount} 项过滤` : '时间窗最大 31 天 · 条件自动记忆' }}
        </span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-seg">
          <button
            v-for="p in PRESETS"
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
            自定义
          </button>
        </div>
        <DatePicker.RangePicker
          :value="customRange"
          :allow-clear="true"
          :show-time="{ format: 'HH:mm:ss' }"
          format="YYYY-MM-DD HH:mm:ss"
          style="width: 340px"
          :placeholder="['开始时间', '结束时间']"
          @change="onRangeChange"
        />
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="advanced = !advanced">
          <IconifyIcon icon="lucide:list-filter" class="mr-1 size-3.5" />
          {{ advanced ? '收起字段' : '更多字段' }}
        </Button>
        <Button size="small" @click="resetFilters">重置</Button>
        <Button type="primary" size="small" @click="query">
          <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />查询
        </Button>
      </div>

      <div class="ipam-filter-grid">
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">关键字</span>
          <Input
            v-model:value="filters.q"
            allow-clear
            placeholder="域名 / MAC / IP 模糊匹配"
            @press-enter="query"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">事件类型</span>
          <Select
            v-model:value="filters.type"
            allow-clear
            placeholder="全部类型"
            :options="[
              { value: 'dns', label: 'DNS 查询' },
              { value: 'dhcp', label: 'DHCP 事件' },
            ]"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">动作</span>
          <Select
            v-model:value="filters.action"
            allow-clear
            show-search
            option-filter-prop="label"
            placeholder="全部动作"
            :options="ACTION_OPTIONS"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">域名</span>
          <Input
            v-model:value="filters.domain"
            allow-clear
            placeholder="子串匹配，如 example.com"
            @press-enter="query"
          />
        </label>

        <template v-if="advanced">
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">客户端 MAC</span>
            <Input
              v-model:value="filters.mac"
              allow-clear
              placeholder="支持冒号或 12 位 hex"
              @press-enter="query"
            />
          </label>
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">客户端 IP</span>
            <Input
              v-model:value="filters.ip"
              allow-clear
              placeholder="精确地址或 CIDR"
              @press-enter="query"
            />
          </label>
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">来源 IP</span>
            <Input
              v-model:value="filters.sip"
              allow-clear
              placeholder="DNS 查询发起方"
              @press-enter="query"
            />
          </label>
          <label class="ipam-filter-field">
            <span class="ipam-filter-field__label">应答 IP</span>
            <Input
              v-model:value="filters.answerIp"
              allow-clear
              placeholder="A/AAAA 应答地址"
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
          检索结果
          <em class="ipam-card-sub">{{ fmtInt(total) }} 条</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">
          点击任意行查看完整字段 · 每 {{ pageSize }} 条一页
        </span>
      </template>

      <Tabs v-model:active-key="activeTab">
        <TabPane key="rows" tab="日志明细">
          <Table
            :columns="columns"
            :custom-row="rowProps"
            :data-source="rows"
            :loading="loading"
            :pagination="{
              current: page,
              pageSize,
              total,
              position: ['bottomLeft'],
              showSizeChanger: true,
              pageSizeOptions: ['20', '50', '100', '200'],
              showTotal: (t: number) => `共 ${t} 条`,
              onChange: onPageChange,
            }"
            :row-key="(r: LogRow) => [r.ts, r.clientMac, r.domain, r.answerIp, r.action].join('|')"
            :scroll="{ x: 920 }"
            :locale="{ emptyText: activeFilterCount > 0 || preset !== '6h' ? '没有匹配当前条件的日志，请调整过滤字段或扩大时间窗' : '当前时间窗暂无日志，等待 DHCP/DNS 事件接入' }"
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

        <TabPane key="domain" tab="热门域名">
          <Table
            :columns="topDomainColumns"
            :data-source="topDomainRows"
            :loading="aggLoading"
            :locale="{ emptyText: '当前过滤条件下没有域名聚合数据' }"
            :pagination="false"
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

        <TabPane key="client" tab="活跃客户端">
          <Table
            :columns="topClientColumns"
            :data-source="topClientRows"
            :loading="aggLoading"
            :locale="{ emptyText: '当前过滤条件下没有客户端聚合数据（DHCP 事件通常携带 MAC）' }"
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

        <TabPane key="qps" tab="QPS 趋势">
          <TrendChart
            v-if="qpsChart.length > 0"
            :data="qpsChart"
            :height="200"
            :markers="true"
            :time-format="preset === '1h' || preset === '6h' ? 'HH:mm' : 'MM-DD HH:mm'"
            tone="blue"
            unit="次"
          />
          <div v-else-if="!aggLoading" class="ipam-empty ipam-empty--compact">
            <div class="ipam-empty__title">暂无趋势数据</div>
            <div class="ipam-empty__desc">当前过滤条件下没有可聚合的事件。</div>
          </div>
          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">窗口总量</span>
              <span class="ipam-figure__value">{{ fmtInt(qpsTotal) }}</span>
              <span class="ipam-figure__unit">次</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">平均每桶</span>
              <span class="ipam-figure__value">{{ qpsAvg.toFixed(1) }}</span>
              <span class="ipam-figure__unit">次</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">峰值每桶</span>
              <span class="ipam-figure__value">{{ fmtInt(qpsPeak) }}</span>
              <span class="ipam-figure__unit">次</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">分桶粒度</span>
              <span class="ipam-figure__value">{{ aggBucketSeconds() }}</span>
              <span class="ipam-figure__unit">秒</span>
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
            <IconifyIcon icon="lucide:copy" class="mr-1 size-3.5" />复制 JSON
          </Button>
        </div>

        <div class="ipam-detail-grid">
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">事件时间</div>
            <div class="ipam-detail-item__value">{{ fmtDateTime(detail.ts) }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">日志级别</div>
            <div class="ipam-detail-item__value">{{ detail.severity || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">域名</div>
            <div class="ipam-detail-item__value">{{ detail.domain || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">动作</div>
            <div class="ipam-detail-item__value">{{ detail.action || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">客户端 MAC</div>
            <div class="ipam-detail-item__value ipam-mono">{{ fmtMac(detail.clientMac) || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">客户端 IP</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.clientIp || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">来源 IP</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.sip || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">应答码 / 分类</div>
            <div class="ipam-detail-item__value">
              {{ detail.rcode || '—' }}
              <span v-if="detail.category" class="ipam-hint"> · {{ detail.category }}</span>
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">应答 IP</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.answerIp || '—' }}</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">原始详情</div>
            <div class="ipam-detail-item__value">{{ detail.detail || '—' }}</div>
          </div>
        </div>
      </template>
    </DetailDrawer>
  </div>
</template>
