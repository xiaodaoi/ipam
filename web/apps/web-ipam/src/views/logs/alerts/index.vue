<script setup lang="ts">
import type { AlertRow, AlertStats } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';

import { useVbenDrawer } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  message,
  Modal,
  Select,
  Table,
  Tag,
} from 'ant-design-vue';

import { ackAlerts, ackAllAlerts, getAlertStats, listAlerts } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';
import { $t } from '#/locales';

const PRESETS = [
  { hours: 1, key: '1h' },
  { hours: 6, key: '6h' },
  { hours: 24, key: '24h' },
  { hours: 168, key: '7d' },
  { hours: 720, key: '30d' },
] as const;
type PresetKey = (typeof PRESETS)[number]['key'];

const presets = computed(() =>
  PRESETS.map((p) => ({ ...p, label: $t(`logs.common.range.${p.key}`) })),
);

const route = useRoute();

const preset = ref<PresetKey>('24h');
const filters = reactive({
  q: '',
  source: undefined as string | undefined,
  severity: undefined as string | undefined,
  status: undefined as string | undefined,
  intelSource: undefined as string | undefined,
});
const page = ref(1);
const pageSize = ref(20);
const rows = ref<AlertRow[]>([]);
const total = ref(0);
const loading = ref(false);
const stats = ref<AlertStats>();
const lastUpdated = ref('');
// 来源下拉候选：无来源过滤时按统计结果刷新，过滤中保持不变（避免下拉只剩当前项）。
const intelSourceOptions = ref<{ label: string; value: string }[]>([]);

const SEVERITY_COLOR: Record<string, string> = {
  critical: 'red',
  warning: 'orange',
  info: 'blue',
};
const STATUS_COLOR: Record<string, string> = {
  acked: 'green',
  open: 'gold',
  resolved: 'default',
};

function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString();
}
function parseTs(v?: null | string): Date | undefined {
  if (!v) return undefined;
  const t = Date.parse(v);
  return Number.isNaN(t) || t <= 0 ? undefined : new Date(t);
}
function fmtDateTime(v?: null | string): string {
  const d = parseTs(v);
  return d ? d.toLocaleString(undefined, { hour12: false }) : '—';
}
function severityLabel(v: string): string {
  return $t(`logs.alerts.severity.${v}`);
}
function statusLabel(v: string): string {
  return $t(`logs.alerts.status.${v}`);
}
function kindLabel(v: string): string {
  return $t(`logs.alerts.kinds.${v}`);
}
function componentLabel(v?: string): string {
  return v ? $t(`logs.alerts.components.${v}`) : '—';
}
function rowSubtitle(a: AlertRow | Record<string, any>): string {
  if (a.source === 'dns') {
    return $t('logs.alerts.row.dnsSubtitle', {
      ip: a.clientIp || '—',
      domain: a.domain || '—',
    });
  }
  return $t('logs.alerts.row.systemSubtitle', {
    component: componentLabel(a.component),
  });
}
function severityCount(v?: string): number {
  return stats.value?.bySeverity?.[v ?? ''] ?? 0;
}

const INTEL_SEPARATOR = ',';
function splitIntelSource(v?: null | string): string[] {
  return (v ?? '')
    .split(INTEL_SEPARATOR)
    .map((x) => x.trim())
    .filter(Boolean);
}
function intelSourceNames(a: AlertRow | Record<string, any>): string[] {
  return splitIntelSource(a.intelSourceName);
}
// 自定义封禁/内置规则/源已删除时没有归属：按 category 展示中性文案，避免空白。
function intelSourceFallback(a: AlertRow | Record<string, any>): string {
  if (a.kind !== 'risk_url') return '—';
  const category = String(a.detail ?? '');
  if (category.includes('intel.custom'))
    return $t('logs.alerts.row.manualBlock');
  if (category.includes('intel.hot') || category.includes('intel.broad')) {
    return $t('logs.alerts.row.builtinIntel');
  }
  return $t('logs.alerts.row.unknownSource');
}

function windowRange(): { from: string; to: string } {
  const p = PRESETS.find((x) => x.key === preset.value) ?? PRESETS[2];
  return {
    from: new Date(Date.now() - p.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
  };
}

function queryParams() {
  return {
    ...windowRange(),
    q: filters.q.trim() || undefined,
    source: filters.source,
    severity: filters.severity,
    status: filters.status,
    intelSource: filters.intelSource,
  };
}

async function load() {
  loading.value = true;
  try {
    const params = queryParams();
    const [p, s] = await Promise.all([
      listAlerts({ ...params, page: page.value, pageSize: pageSize.value }),
      getAlertStats(params),
    ]);
    rows.value = p.items ?? [];
    total.value = p.total ?? rows.value.length;
    stats.value = s;
    if (!filters.intelSource) {
      intelSourceOptions.value = Object.entries(s.byIntelSource ?? {})
        .toSorted((a, b) => b[1] - a[1])
        .map(([name]) => ({ label: name, value: name }));
    }
    lastUpdated.value = new Date().toLocaleTimeString(undefined, {
      hour12: false,
    });
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.alerts.messages.loadFail'),
    );
  } finally {
    loading.value = false;
  }
}

function query() {
  page.value = 1;
  void load();
}

function reset() {
  filters.q = '';
  filters.source = undefined;
  filters.severity = undefined;
  filters.status = undefined;
  filters.intelSource = undefined;
  preset.value = '24h';
  page.value = 1;
  void load();
}

function onPreset(key: PresetKey) {
  preset.value = key;
  page.value = 1;
  void load();
}

function onTableChange(p: any) {
  page.value = p.current ?? 1;
  pageSize.value = p.pageSize ?? pageSize.value;
  void load();
}

function applyRowAck(id: string) {
  const at = new Date().toISOString();
  const row = rows.value.find((r) => r.id === id);
  if (row) {
    row.status = 'acked';
    row.ackedAt = at;
  }
  if (detail.value?.id === id) {
    detail.value.status = 'acked';
    detail.value.ackedAt = at;
  }
}

async function ackOne(a: AlertRow): Promise<boolean> {
  if (a.status !== 'open') return true;
  try {
    await ackAlerts([a.id]);
    applyRowAck(a.id);
    if (stats.value) {
      stats.value.open = Math.max(0, (stats.value.open ?? 0) - 1);
      stats.value.acked = (stats.value.acked ?? 0) + 1;
    }
    return true;
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.alerts.messages.ackFail'),
    );
    return false;
  }
}

const detail = ref<AlertRow>();
const [DetailDrawer, detailDrawerApi] = useVbenDrawer({
  class: 'w-[620px]',
  footer: false,
  title: $t('logs.alerts.detail.title'),
});

async function openDetail(a: AlertRow) {
  detail.value = a;
  await ackOne(a);
  detailDrawerApi.open();
}

function rowProps(record: Record<string, any>) {
  return {
    class: 'ipam-row-link',
    onClick: () => void openDetail(record as AlertRow),
  };
}

async function ackVisible() {
  const open = rows.value.filter((r) => r.status === 'open');
  if (open.length === 0) {
    message.info($t('logs.alerts.messages.noUnread'));
    return;
  }
  try {
    const res = await ackAlerts(open.map((r) => r.id));
    message.success($t('logs.alerts.messages.acked', { n: res.acked }));
    void load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.alerts.messages.ackFail'),
    );
  }
}

function onAckAll() {
  Modal.confirm({
    title: $t('logs.alerts.actions.ackAll'),
    content: $t('logs.alerts.messages.confirmAckAll'),
    okText: $t('logs.alerts.actions.ackAll'),
    cancelText: $t('logs.alerts.detail.close'),
    onOk: async () => {
      try {
        const res = await ackAllAlerts();
        message.success($t('logs.alerts.messages.acked', { n: res.acked }));
        await load();
      } catch (error) {
        message.error(
          error instanceof Error
            ? error.message
            : $t('logs.alerts.messages.ackFail'),
        );
      }
    },
  });
}

const columns = computed(() => [
  {
    dataIndex: 'severity',
    key: 'severity',
    title: $t('logs.alerts.table.severity'),
    width: 92,
  },
  { dataIndex: 'title', key: 'title', title: $t('logs.alerts.table.alert') },
  {
    dataIndex: 'source',
    key: 'source',
    title: $t('logs.alerts.table.source'),
    width: 130,
  },
  {
    dataIndex: 'intelSourceName',
    key: 'intelSource',
    title: $t('logs.alerts.table.intelSource'),
    width: 220,
  },
  {
    dataIndex: 'clientIp',
    key: 'clientIp',
    title: $t('logs.alerts.table.client'),
    width: 150,
  },
  {
    dataIndex: 'count',
    key: 'count',
    title: $t('logs.alerts.table.count'),
    width: 90,
  },
  {
    dataIndex: 'status',
    key: 'status',
    title: $t('logs.alerts.table.status'),
    width: 100,
  },
  {
    dataIndex: 'lastSeen',
    key: 'lastSeen',
    title: $t('logs.alerts.table.lastSeen'),
    width: 180,
  },
]);

const trendData = computed(() =>
  (stats.value?.trend ?? []).map((p) => ({ ts: p.ts, value: p.count })),
);
const topDomains = computed(() => stats.value?.topDomains ?? []);
const topClients = computed(() => stats.value?.topClients ?? []);
const topDomainMax = computed(() =>
  Math.max(1, ...topDomains.value.map((d) => d.count)),
);
const topClientMax = computed(() =>
  Math.max(1, ...topClients.value.map((d) => d.count)),
);
const topSources = computed(() =>
  Object.entries(stats.value?.byIntelSource ?? {})
    .map(([key, count]) => ({ key, count }))
    .toSorted((a, b) => b.count - a.count)
    .slice(0, 5),
);
const topSourceMax = computed(() =>
  Math.max(1, ...topSources.value.map((d) => d.count)),
);

function activeFilterCount(): number {
  return [
    filters.q.trim(),
    filters.source,
    filters.severity,
    filters.status,
    filters.intelSource,
  ].filter(Boolean).length;
}

async function openFromRoute() {
  const id = typeof route.query.alertId === 'string' ? route.query.alertId : '';
  if (!id) return;
  const row = rows.value.find((r) => r.id === id);
  if (row) await openDetail(row);
}

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(async () => {
  await load();
  await openFromRoute();
  timer = setInterval(() => {
    if (!loading.value) void load();
  }, 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Alert Center"
      icon="lucide:bell-ring"
      :title="$t('logs.alerts.title')"
      :description="$t('logs.alerts.description')"
    >
      <template #actions>
        <span v-if="lastUpdated" class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock" class="size-3.5" />
          {{ lastUpdated }}
        </span>
        <Button size="small" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('logs.alerts.actions.refresh')
          }}
        </Button>
        <Button size="small" @click="ackVisible">
          <IconifyIcon icon="lucide:check-check" class="mr-1 size-3.5" />{{
            $t('logs.alerts.actions.ackPage')
          }}
        </Button>
        <Button size="small" type="primary" @click="onAckAll">
          <IconifyIcon icon="lucide:bell-off" class="mr-1 size-3.5" />{{
            $t('logs.alerts.actions.ackAll')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('logs.alerts.kpi.total')"
        :value="fmtInt(stats?.total)"
        icon="lucide:bell-ring"
        tone="blue"
        :hint="$t('logs.alerts.kpi.totalHint')"
      />
      <StatCard
        :label="$t('logs.alerts.kpi.open')"
        :value="fmtInt(stats?.open)"
        icon="lucide:bell-dot"
        :tone="(stats?.open ?? 0) > 0 ? 'amber' : 'green'"
        :hint="$t('logs.alerts.kpi.openHint')"
      />
      <StatCard
        :label="$t('logs.alerts.kpi.critical')"
        :value="fmtInt(severityCount('critical'))"
        icon="lucide:triangle-alert"
        :tone="severityCount('critical') > 0 ? 'red' : 'green'"
        :hint="$t('logs.alerts.kpi.criticalHint')"
      />
      <StatCard
        :label="$t('logs.alerts.kpi.domains')"
        :value="fmtInt(stats?.byKind?.risk_url)"
        icon="lucide:shield-ban"
        tone="violet"
        :hint="$t('logs.alerts.kpi.domainsHint')"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:sliders-horizontal" />
          {{ $t('logs.alerts.filters.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">
          {{
            activeFilterCount() > 0
              ? $t('logs.alerts.filters.active', { n: activeFilterCount() })
              : ''
          }}
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
        </div>
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="reset">
          {{ $t('logs.alerts.filters.reset') }}
        </Button>
        <Button type="primary" size="small" @click="query">
          <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />{{
            $t('logs.common.query')
          }}
        </Button>
      </div>

      <div class="ipam-filter-grid">
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.alerts.filters.keyword')
          }}</span>
          <Input
            v-model:value="filters.q"
            allow-clear
            :placeholder="$t('logs.alerts.filters.keywordPlaceholder')"
            @press-enter="query"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.alerts.filters.source')
          }}</span>
          <Select
            v-model:value="filters.source"
            allow-clear
            :placeholder="$t('logs.alerts.filters.all')"
            :options="[
              { value: 'dns', label: $t('logs.alerts.source.dns') },
              { value: 'system', label: $t('logs.alerts.source.system') },
            ]"
            @change="query"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.alerts.filters.intelSource')
          }}</span>
          <Select
            v-model:value="filters.intelSource"
            allow-clear
            show-search
            :placeholder="$t('logs.alerts.filters.allSources')"
            :options="intelSourceOptions"
            @change="query"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.alerts.filters.severity')
          }}</span>
          <Select
            v-model:value="filters.severity"
            allow-clear
            :placeholder="$t('logs.alerts.filters.all')"
            :options="[
              { value: 'critical', label: $t('logs.alerts.severity.critical') },
              { value: 'warning', label: $t('logs.alerts.severity.warning') },
              { value: 'info', label: $t('logs.alerts.severity.info') },
            ]"
            @change="query"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.alerts.filters.status')
          }}</span>
          <Select
            v-model:value="filters.status"
            allow-clear
            :placeholder="$t('logs.alerts.filters.all')"
            :options="[
              { value: 'open', label: $t('logs.alerts.status.open') },
              { value: 'acked', label: $t('logs.alerts.status.acked') },
              { value: 'resolved', label: $t('logs.alerts.status.resolved') },
            ]"
            @change="query"
          />
        </label>
      </div>
    </Card>

    <div class="grid items-start gap-4 lg:grid-cols-3">
      <Card class="lg:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:trending-up" />
            {{ $t('logs.alerts.trend.title') }}
          </span>
        </template>
        <TrendChart
          v-if="trendData.length > 0"
          :data="trendData"
          :height="200"
          :unit="$t('logs.alerts.trend.unit')"
          :time-format="preset === '1h' || preset === '6h' ? 'HH:mm' : 'MM-DD'"
        />
        <div v-else class="ipam-empty">{{ $t('logs.common.noTrend') }}</div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:radar" />
            {{ $t('logs.alerts.trend.sources') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{
            $t('logs.alerts.trend.sourcesHint')
          }}</span>
        </template>
        <div v-if="topSources.length > 0" class="alert-rank-list">
          <div
            v-for="(i, idx) in topSources"
            :key="i.key"
            class="alert-rank-row"
          >
            <span class="alert-rank-no" :data-rank="idx + 1">
              {{ idx + 1 }}
            </span>
            <div class="alert-rank-main">
              <div class="alert-rank-head">
                <span class="alert-rank-label" :title="i.key">{{ i.key }}</span>
                <span class="alert-rank-count">{{ fmtInt(i.count) }}</span>
              </div>
              <div class="alert-rank-track">
                <span
                  :style="{ width: `${(i.count / topSourceMax) * 100}%` }"
                ></span>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="alert-rank-empty">
          <IconifyIcon icon="lucide:radar" />
          <span>{{ $t('logs.alerts.trend.empty') }}</span>
        </div>
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list" />
          {{ $t('logs.alerts.table.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ fmtInt(total) }}</span>
      </template>
      <Table
        :columns="columns"
        :data-source="rows"
        :loading="loading"
        :row-key="(r: AlertRow) => r.id"
        :custom-row="rowProps"
        :pagination="{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          pageSizeOptions: ['20', '50', '100'],
          showTotal: (t: number) => `${t}`,
        }"
        :scroll="{ x: 1000 }"
        size="middle"
        @change="onTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'severity'">
            <Tag :color="SEVERITY_COLOR[record.severity] ?? 'default'">
              {{ severityLabel(record.severity) }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'title'">
            <div class="font-medium">{{ kindLabel(record.kind) }}</div>
            <div class="ipam-hint">{{ rowSubtitle(record) }}</div>
          </template>
          <template v-else-if="column.key === 'source'">
            <Tag :color="record.source === 'dns' ? 'volcano' : 'geekblue'">
              {{ $t(`logs.alerts.source.${record.source}`) }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'intelSource'">
            <div
              v-if="intelSourceNames(record).length > 0"
              class="flex flex-wrap gap-1"
            >
              <Tag
                v-for="name in intelSourceNames(record)"
                :key="name"
                :color="
                  record.intelSource?.startsWith('custom:')
                    ? 'default'
                    : 'purple'
                "
                class="m-0"
              >
                {{ name }}
              </Tag>
            </div>
            <span v-else class="ipam-hint">{{
              intelSourceFallback(record)
            }}</span>
          </template>
          <template v-else-if="column.key === 'clientIp'">
            <span class="ipam-mono">{{ record.clientIp || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'count'">
            {{ fmtInt(record.count) }}
          </template>
          <template v-else-if="column.key === 'status'">
            <Tag :color="STATUS_COLOR[record.status] ?? 'default'">
              {{ statusLabel(record.status) }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'lastSeen'">
            <span class="ipam-mono text-xs">
              {{ fmtDateTime(record.lastSeen) }}
            </span>
          </template>
        </template>
        <template #emptyText>
          <div class="ipam-empty">{{ $t('logs.alerts.table.empty') }}</div>
        </template>
      </Table>
    </Card>

    <div class="grid gap-4 lg:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:shield-ban" />
            {{ $t('logs.alerts.trend.domains') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{
            $t('logs.alerts.trend.domainsHint')
          }}</span>
        </template>
        <div v-if="topDomains.length > 0" class="alert-rank-list">
          <div
            v-for="(d, idx) in topDomains"
            :key="d.key"
            class="alert-rank-row"
          >
            <span class="alert-rank-no" :data-rank="idx + 1">
              {{ idx + 1 }}
            </span>
            <div class="alert-rank-main">
              <div class="alert-rank-head">
                <span class="alert-rank-label" :title="d.key">{{ d.key }}</span>
                <span class="alert-rank-count">
                  {{ fmtInt(d.count) }}
                  <small>{{ $t('logs.alerts.trend.hits') }}</small>
                </span>
              </div>
              <div class="alert-rank-track">
                <span
                  :style="{ width: `${(d.count / topDomainMax) * 100}%` }"
                ></span>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="alert-rank-empty">
          <IconifyIcon icon="lucide:shield-ban" />
          <span>{{ $t('logs.alerts.trend.empty') }}</span>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:users" />
            {{ $t('logs.alerts.trend.clients') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{
            $t('logs.alerts.trend.clientsHint')
          }}</span>
        </template>
        <div v-if="topClients.length > 0" class="alert-rank-list">
          <div
            v-for="(c, idx) in topClients"
            :key="c.key"
            class="alert-rank-row"
          >
            <span class="alert-rank-no" :data-rank="idx + 1">
              {{ idx + 1 }}
            </span>
            <div class="alert-rank-main">
              <div class="alert-rank-head">
                <span class="alert-rank-label" :title="c.key">{{ c.key }}</span>
                <span class="alert-rank-count">
                  {{ fmtInt(c.count) }}
                  <small>{{ $t('logs.alerts.trend.hits') }}</small>
                </span>
              </div>
              <div class="alert-rank-track">
                <span
                  :style="{ width: `${(c.count / topClientMax) * 100}%` }"
                ></span>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="alert-rank-empty">
          <IconifyIcon icon="lucide:users" />
          <span>{{ $t('logs.alerts.trend.empty') }}</span>
        </div>
      </Card>
    </div>

    <DetailDrawer>
      <template v-if="detail">
        <div class="ipam-detail-head">
          <Tag
            :color="SEVERITY_COLOR[detail.severity] ?? 'default'"
            class="m-0"
          >
            {{ severityLabel(detail.severity) }}
          </Tag>
          <Tag :color="STATUS_COLOR[detail.status] ?? 'default'" class="m-0">
            {{ statusLabel(detail.status) }}
          </Tag>
          <span class="font-medium">{{ kindLabel(detail.kind) }}</span>
          <span class="ipam-toolbar__spacer"></span>
          <Button
            v-if="detail.status === 'open'"
            size="small"
            type="primary"
            @click="ackOne(detail)"
          >
            {{ $t('logs.alerts.detail.ack') }}
          </Button>
        </div>

        <div class="ipam-detail-grid">
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.firstSeen') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ fmtDateTime(detail.firstSeen) }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.lastSeen') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ fmtDateTime(detail.lastSeen) }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.client') }}
            </div>
            <div class="ipam-detail-item__value ipam-mono">
              {{ detail.clientIp || '—' }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.domain') }}
            </div>
            <div class="ipam-detail-item__value ipam-mono">
              {{ detail.domain || '—' }}
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.intelSource') }}
            </div>
            <div class="ipam-detail-item__value">
              <template v-if="intelSourceNames(detail).length > 0">
                <Tag
                  v-for="name in intelSourceNames(detail)"
                  :key="name"
                  color="purple"
                  class="m-0 mr-1"
                >
                  {{ name }}
                </Tag>
              </template>
              <span v-else>{{ intelSourceFallback(detail) }}</span>
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.component') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ componentLabel(detail.component) }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.count') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ fmtInt(detail.count) }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.ackedAt') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ fmtDateTime(detail.ackedAt) }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.ackedBy') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ detail.ackedBy || '—' }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.resolvedAt') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ fmtDateTime(detail.resolvedAt) }}
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.alerts.detail.detail') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ detail.detail || detail.message || '—' }}
            </div>
          </div>
        </div>
      </template>
    </DetailDrawer>
  </div>
</template>

<style scoped>
.alert-rank-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.alert-rank-row {
  display: grid;
  grid-template-columns: 26px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  padding: 7px 6px;
  border-radius: 8px;
  transition: background-color 0.15s ease;
}

.alert-rank-row:hover {
  background: hsl(var(--accent));
}

.alert-rank-no {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  font-size: 11px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
  border-radius: 6px;
}

.alert-rank-row:nth-child(1) .alert-rank-no {
  color: #92400e;
  background: #fef3c7;
}

.alert-rank-row:nth-child(2) .alert-rank-no {
  color: #475569;
  background: #e2e8f0;
}

.alert-rank-row:nth-child(3) .alert-rank-no {
  color: #9a3412;
  background: #ffedd5;
}

.alert-rank-main {
  min-width: 0;
}

.alert-rank-head {
  display: flex;
  gap: 12px;
  align-items: baseline;
  justify-content: space-between;
  min-width: 0;
}

.alert-rank-label {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family:
    ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono',
    monospace;
  font-size: 12.5px;
  line-height: 1.4;
  color: hsl(var(--foreground));
  white-space: nowrap;
}

.alert-rank-count {
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: hsl(var(--foreground));
  white-space: nowrap;
}

.alert-rank-count small {
  margin-left: 3px;
  font-size: 10px;
  font-weight: 400;
  color: hsl(var(--muted-foreground));
}

.alert-rank-track {
  height: 4px;
  margin-top: 5px;
  overflow: hidden;
  background: hsl(var(--muted));
  border-radius: 9999px;
}

.alert-rank-track > span {
  display: block;
  height: 100%;
  background: linear-gradient(
    90deg,
    hsl(var(--primary)),
    hsl(var(--primary) / 55%)
  );
  border-radius: 9999px;
  transition: width 0.3s ease;
}

.alert-rank-empty {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  justify-content: center;
  min-height: 120px;
  font-size: 12px;
  color: hsl(var(--muted-foreground));
}

.alert-rank-empty svg {
  width: 22px;
  height: 22px;
  opacity: 0.35;
}
</style>
