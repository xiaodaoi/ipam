<script setup lang="ts">
import type { AuditEntry, TopEntry } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Table, Tag } from 'ant-design-vue';

import { getLogQps, listAudits, listLogTop } from '#/api/ipam';
import MetricRing from '#/components/metric-ring.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';
import { $t } from '#/locales';

import { useLogWindowStats } from '../use-log-window-stats';

const RANGE_HOURS = [
  { hours: 1, key: '1h' },
  { hours: 6, key: '6h' },
  { hours: 24, key: '24h' },
  { hours: 168, key: '7d' },
] as const;
type RangeKey = (typeof RANGE_HOURS)[number]['key'];

const ranges = computed(() =>
  RANGE_HOURS.map((r) => ({
    ...r,
    label: $t(`logs.common.range.${r.key}`),
  })),
);

const range = ref<RangeKey>('6h');
const window = ref({
  from: '',
  to: '',
  labelKey: 'logs.common.range.6h',
});
const windowLabel = computed(() => $t(window.value.labelKey));

function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString('zh-CN');
}
function fmtTime(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleTimeString('zh-CN', { hour12: false });
}
function fmtDate(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleDateString('zh-CN');
}
function fmtMac(mac?: string): string {
  if (!mac) return '';
  const hex = mac.replaceAll(/[^0-9a-f]/gi, '').toLowerCase();
  return hex.length === 12 ? (hex.match(/.{2}/g) ?? []).join(':') : mac;
}
function pct(part: number, all: number): string {
  return all ? `${((part / all) * 100).toFixed(1)}%` : '0%';
}

function refreshWindow() {
  const r = RANGE_HOURS.find((x) => x.key === range.value) ?? RANGE_HOURS[1]!;
  window.value = {
    from: new Date(Date.now() - r.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
    labelKey: `logs.common.range.${r.key}`,
  };
}

// ── KPI（复用检索页同口径统计）──
const kpi = useLogWindowStats();
const stats = computed(() => kpi.stats.value);

// ── 趋势 / TopN / 最近审计 ──
const qpsPoints = ref<{ ts: string; value: number }[]>([]);
const topDomains = ref<TopEntry[]>([]);
const topClients = ref<TopEntry[]>([]);
const recentAudits = ref<AuditEntry[]>([]);
const loading = ref(false);
const lastUpdated = ref('');

const qpsTotal = computed(() =>
  qpsPoints.value.reduce((sum, p) => sum + p.value, 0),
);
const qpsPeak = computed(() =>
  Math.max(0, ...qpsPoints.value.map((p) => p.value)),
);
const qpsAvg = computed(() =>
  qpsPoints.value.length > 0 ? qpsTotal.value / qpsPoints.value.length : 0,
);
const bucketSeconds = computed(() => {
  const start = Date.parse(window.value.from);
  const end = Date.parse(window.value.to);
  return Math.max(60, Math.round((end - start) / 1000 / 48));
});
const topDomainMax = computed(() =>
  Math.max(1, ...topDomains.value.map((d) => d.count)),
);
const topClientMax = computed(() =>
  Math.max(1, ...topClients.value.map((d) => d.count)),
);

const dnsShare = computed(() =>
  stats.value?.total
    ? Math.round(((stats.value.dns ?? 0) / stats.value.total) * 100)
    : 0,
);
const otherCount = computed(() => {
  const s = stats.value;
  return s ? Math.max(0, s.total - s.dns - s.dhcp) : 0;
});

async function loadAll() {
  refreshWindow();
  loading.value = true;
  const base = { from: window.value.from, to: window.value.to };
  const [qps, domains, clients, audits] = await Promise.allSettled([
    getLogQps({ ...base, intervalSec: bucketSeconds.value }),
    listLogTop({ ...base, by: 'domain', limit: 8 }),
    listLogTop({ ...base, by: 'client', limit: 8 }),
    listAudits({ from: base.from, to: base.to, pageSize: 8 }),
  ]);
  qpsPoints.value =
    qps.status === 'fulfilled'
      ? (qps.value.points ?? []).map((p) => ({ ts: p.ts, value: p.count }))
      : [];
  topDomains.value = domains.status === 'fulfilled' ? (domains.value.items ?? []) : [];
  topClients.value = clients.status === 'fulfilled' ? (clients.value.items ?? []) : [];
  recentAudits.value = audits.status === 'fulfilled' ? (audits.value.items ?? []) : [];
  lastUpdated.value = new Date().toLocaleTimeString('zh-CN', { hour12: false });
  loading.value = false;
  void kpi.load(base);
}

function setRange(key: RangeKey) {
  range.value = key;
  void loadAll();
}

const auditColumns = computed(() => [
  { dataIndex: 'ts', key: 'ts', title: $t('logs.overview.columns.time'), width: 96 },
  { dataIndex: 'actor', key: 'actor', title: $t('logs.overview.columns.actor'), width: 150 },
  { dataIndex: 'actorType', key: 'actorType', title: $t('logs.overview.columns.type'), width: 76 },
  { dataIndex: 'action', key: 'action', title: $t('logs.overview.columns.action'), width: 92 },
  { dataIndex: 'resource', key: 'resource', title: $t('logs.overview.columns.resource'), ellipsis: true },
  { dataIndex: 'status', key: 'status', title: $t('logs.overview.columns.status'), width: 76 },
]);
const ACTOR_META = computed<Record<string, { color: string; label: string }>>(
  () => ({
    human: { color: 'blue', label: $t('logs.overview.actor.human') },
    bot: { color: 'purple', label: $t('logs.overview.actor.bot') },
    system: { color: 'default', label: $t('logs.overview.actor.system') },
  }),
);
const ACTION_COLOR: Record<string, string> = {
  create: 'green',
  delete: 'red',
  update: 'blue',
  login: 'cyan',
  logout: 'default',
};
function statusTone(s?: number): 'amber' | 'default' | 'green' | 'red' {
  if (!s) return 'default';
  if (s < 400) return 'green';
  if (s >= 500) return 'red';
  return 'amber';
}

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  void loadAll();
  timer = setInterval(() => void loadAll(), 60_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:layout-dashboard"
      :title="$t('logs.overview.title')"
      :description="$t('logs.overview.description')"
    >
      <template #actions>
        <div class="ipam-seg">
          <button
            v-for="r in ranges"
            :key="r.key"
            type="button"
            :data-active="range === r.key"
            @click="setRange(r.key)"
          >
            {{ r.label }}
          </button>
        </div>
        <span class="ipam-page-header__meta">
          {{ $t('logs.overview.updatedAt', { time: lastUpdated || '—' }) }}
        </span>
        <Button size="small" :loading="loading" @click="loadAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{ $t('logs.common.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('logs.overview.kpi.total')"
        :value="stats ? fmtInt(stats.total) : kpi.failed.value ? '—' : '…'"
        icon="lucide:activity"
        tone="blue"
        :spark="stats?.spark"
        :hint="$t('logs.overview.kpi.totalHint', { range: windowLabel })"
      />
      <StatCard
        :label="$t('logs.overview.kpi.dns')"
        :value="stats ? fmtInt(stats.dns) : kpi.failed.value ? '—' : '…'"
        icon="lucide:globe"
        tone="cyan"
        :hint="$t('logs.common.share', { pct: pct(stats?.dns ?? 0, stats?.total ?? 0) })"
      />
      <StatCard
        :label="$t('logs.overview.kpi.dhcp')"
        :value="stats ? fmtInt(stats.dhcp) : kpi.failed.value ? '—' : '…'"
        icon="lucide:network"
        tone="green"
        :hint="$t('logs.common.share', { pct: pct(stats?.dhcp ?? 0, stats?.total ?? 0) })"
      />
      <StatCard
        :label="$t('logs.overview.kpi.blocked')"
        :value="stats ? fmtInt(stats.blocked) : kpi.failed.value ? '—' : '…'"
        icon="lucide:shield-ban"
        :tone="(stats?.blocked ?? 0) > 0 ? 'red' : 'violet'"
        :hint="(stats?.blocked ?? 0) > 0 ? $t('logs.common.blockedShare', { pct: pct(stats?.blocked ?? 0, stats?.dns ?? 0) }) : $t('logs.common.noBlocked')"
      />
    </div>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:chart-area" />
            {{ $t('logs.overview.trend.title') }}
            <em class="ipam-card-sub">{{ $t('logs.overview.trend.bucket', { n: bucketSeconds }) }}</em>
          </span>
        </template>
        <template #extra>
          <span class="ipam-card-legend">
            <span><i data-kind="peak"></i>{{ $t('logs.overview.trend.peak') }}</span>
            <span><i data-kind="avg"></i>{{ $t('logs.overview.trend.avg') }}</span>
          </span>
        </template>

        <TrendChart
          v-if="qpsPoints.length > 0"
          :data="qpsPoints"
          :height="210"
          :time-format="range === '1h' || range === '6h' ? 'HH:mm' : 'MM-DD HH:mm'"
          tone="cyan"
          :unit="$t('logs.common.times')"
        />
        <div v-else class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:chart-area" class="size-5" />
          </div>
          <div class="ipam-empty__title">{{ $t('logs.common.noTrend') }}</div>
          <div class="ipam-empty__desc">
            {{ $t('logs.overview.trend.emptyDesc') }}
          </div>
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
            <span class="ipam-figure__label">{{ $t('logs.common.figures.blockedShare') }}</span>
            <span class="ipam-figure__value">{{ pct(stats?.blocked ?? 0, stats?.total ?? 0) }}</span>
            <span class="ipam-figure__unit">{{ $t('logs.common.figures.allEvents') }}</span>
          </div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:pie-chart" />
            {{ $t('logs.overview.compose.title') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ windowLabel }}</span>
        </template>

        <div v-if="stats && stats.total > 0" class="ipam-compose">
          <MetricRing
            :value="dnsShare"
            :size="112"
            :thickness="10"
            tone="cyan"
            :label="`${dnsShare}%`"
            :caption="$t('logs.overview.compose.dnsShare')"
          />
          <div class="ipam-dist">
            <div class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>{{ $t('logs.overview.compose.dns') }}</span>
                <span>{{ fmtInt(stats.dns) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="cyan" :style="{ width: `${Math.max(2, (stats.dns / stats.total) * 100)}%` }"></i>
              </div>
            </div>
            <div class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>{{ $t('logs.overview.compose.dhcp') }}</span>
                <span>{{ fmtInt(stats.dhcp) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="green" :style="{ width: `${Math.max(2, (stats.dhcp / stats.total) * 100)}%` }"></i>
              </div>
            </div>
            <div class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>{{ $t('logs.overview.compose.blocked') }}</span>
                <span>{{ fmtInt(stats.blocked) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="red" :style="{ width: `${Math.max(2, (stats.blocked / Math.max(1, stats.dns)) * 100)}%` }"></i>
              </div>
            </div>
            <div v-if="otherCount > 0" class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>{{ $t('logs.overview.compose.other') }}</span>
                <span>{{ fmtInt(otherCount) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="amber" :style="{ width: `${Math.max(2, (otherCount / stats.total) * 100)}%` }"></i>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">{{ $t('logs.overview.compose.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('logs.overview.compose.emptyDesc') }}</div>
        </div>

        <div class="ipam-note mt-3">
          <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
          <span>{{ $t('logs.overview.compose.note') }}</span>
        </div>
      </Card>
    </div>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:flame" />
            {{ $t('logs.common.topDomains') }}
            <em class="ipam-card-sub">Top 8</em>
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/logs-center/search">
            {{ $t('logs.common.rowsDetail') }}
            <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
          </RouterLink>
        </template>

        <div v-if="topDomains.length > 0" class="ipam-rank">
          <div v-for="(d, i) in topDomains" :key="d.key" class="ipam-rank__item">
            <span class="ipam-rank__no" :data-top="i < 3">{{ i + 1 }}</span>
            <div class="ipam-rank__body">
              <div class="ipam-rank__title">
                <span class="ipam-rank__name ipam-mono" :title="d.key">{{ d.key }}</span>
                <span class="ipam-rank__value">{{ fmtInt(d.count) }}</span>
              </div>
              <div class="ipam-progress ipam-rank__bar">
                <i :style="{ width: `${Math.max(4, (d.count / topDomainMax) * 100)}%` }"></i>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">{{ $t('logs.overview.topDomains.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('logs.overview.topDomains.emptyDesc') }}</div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:monitor-smartphone" />
            {{ $t('logs.common.topClients') }}
            <em class="ipam-card-sub">{{ $t('logs.overview.topClients.subtitle') }}</em>
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/logs-center/search">
            {{ $t('logs.common.rowsDetail') }}
            <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
          </RouterLink>
        </template>

        <div v-if="topClients.length > 0" class="ipam-rank">
          <div v-for="(d, i) in topClients" :key="d.key" class="ipam-rank__item">
            <span class="ipam-rank__no" :data-top="i < 3">{{ i + 1 }}</span>
            <div class="ipam-rank__body">
              <div class="ipam-rank__title">
                <span class="ipam-rank__name ipam-mono" :title="d.key">{{ fmtMac(d.key) }}</span>
                <span class="ipam-rank__value">{{ fmtInt(d.count) }}</span>
              </div>
              <div class="ipam-progress ipam-rank__bar">
                <i :style="{ width: `${Math.max(4, (d.count / topClientMax) * 100)}%` }"></i>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">{{ $t('logs.overview.topClients.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('logs.overview.topClients.emptyDesc') }}</div>
        </div>
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />
          {{ $t('logs.overview.recentAudit.title') }}
          <em class="ipam-card-sub">{{ windowLabel }}</em>
        </span>
      </template>
      <template #extra>
        <RouterLink class="ipam-link" to="/logs-center/audit">
          {{ $t('logs.overview.recentAudit.viewAll') }}
          <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
        </RouterLink>
      </template>

      <Table
        :columns="auditColumns"
        :data-source="recentAudits"
        :locale="{ emptyText: $t('logs.overview.recentAudit.empty') }"
        :pagination="false"
        :row-key="(r: AuditEntry) => String(r.id)"
        :scroll="{ x: 820 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'ts'">
            <span class="ipam-log-time">
              <b>{{ fmtTime(record.ts) }}</b>
              <span>{{ fmtDate(record.ts) }}</span>
            </span>
          </template>
          <template v-else-if="column.key === 'actor'">
            <span class="ipam-cell-title">{{ record.actor || 'anonymous' }}</span>
          </template>
          <template v-else-if="column.key === 'actorType'">
            <Tag class="m-0" :color="ACTOR_META[record.actorType]?.color">
              {{ ACTOR_META[record.actorType]?.label ?? record.actorType }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <Tag class="m-0" :color="ACTION_COLOR[record.action] ?? 'default'">
              {{ record.action }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'resource'">
            <span class="ipam-cell-stack">
              <span class="ipam-cell-title" :title="record.resource">{{ record.resource }}</span>
              <span class="ipam-cell-meta ipam-mono">{{ record.method }} {{ record.path }}</span>
            </span>
          </template>
          <template v-else-if="column.key === 'status'">
            <Tag class="m-0" :color="statusTone(record.status)">{{ record.status }}</Tag>
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>
