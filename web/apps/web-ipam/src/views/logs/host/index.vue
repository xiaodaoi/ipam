<script setup lang="ts">
import type { Dayjs } from 'dayjs';

import type {
  HostMetrics,
  SystemMetricPoint,
  SystemMetricSummary,
} from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, DatePicker, message, Table, Tag } from 'ant-design-vue';

import {
  getSystemMetrics,
  getSystemMetricsHistory,
  getSystemMetricsSummary,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import PieChart from '#/components/pie-chart.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';
import { $t } from '#/locales';

defineOptions({ name: 'LogsHost' });

/** 预设区间 → 小时数与期望聚合粒度（秒）。 */
const RANGE_CONF = {
  '1h': { hours: 1, step: 60 },
  '6h': { hours: 6, step: 60 },
  '7d': { hours: 168, step: 600 },
  '24h': { hours: 24, step: 120 },
  '30d': { hours: 720, step: 1800 },
} as const;
type RangeKey = keyof typeof RANGE_CONF;
const RANGE_KEYS: RangeKey[] = ['1h', '6h', '24h', '7d', '30d'];

const range = ref<string>('24h');
const customRange = ref<[Dayjs, Dayjs]>();
const windowRange = ref({
  from: new Date(Date.now() - 24 * 3_600_000).toISOString(),
  to: new Date().toISOString(),
});
const windowLabel = computed(() =>
  range.value === 'custom'
    ? $t('logs.common.customRange')
    : $t(`logs.common.range.${range.value}`),
);
const ranges = computed(() =>
  RANGE_KEYS.map((key) => ({
    key,
    label: $t(`logs.common.range.${key}`),
  })),
);

const metrics = ref<HostMetrics>();
const points = ref<SystemMetricPoint[]>([]);
const summary = ref<SystemMetricSummary>();
const loading = ref(false);
const snapshotLoading = ref(false);
const lastUpdated = ref('');

// ── 格式化 ──────────────────────────────────────────────────────────────
function fmtPct(v?: null | number): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${Number(v).toFixed(1)}%`;
}
function fmtBytes(v?: null | number): string {
  if (!v || v <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let n = Number(v);
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(i === 0 || n >= 100 ? 0 : 1)} ${units[i]}`;
}
function fmtDateTime(v?: null | string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString(undefined, { hour12: false });
}
function fmtTime(v?: null | string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleTimeString(undefined, { hour12: false });
}
function fmtUptime(sec?: null | number): string {
  if (!sec || sec <= 0) return '—';
  const d = Math.floor(sec / 86_400);
  const h = Math.floor((sec % 86_400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return $t('logs.host.uptime.days', { d, h });
  if (h > 0) return $t('logs.host.uptime.hours', { h, m });
  return $t('logs.host.uptime.minutes', { m });
}

// ── 当前快照 ────────────────────────────────────────────────────────────
const cpuPct = computed(() => Number(metrics.value?.cpu?.usagePct ?? 0));
const memPct = computed(() => Number(metrics.value?.memory?.usedPct ?? 0));
const diskPct = computed(() =>
  Math.max(0, ...(metrics.value?.disks ?? []).map((d) => d.usedPct ?? 0)),
);
const worstDisk = computed(() => {
  const disks = metrics.value?.disks ?? [];
  let worst: (typeof disks)[number] | undefined;
  for (const d of disks) {
    if (!worst || (d.usedPct ?? 0) > (worst.usedPct ?? 0)) worst = d;
  }
  return worst;
});
const load1 = computed(() => Number(metrics.value?.cpu?.load1 ?? 0));
const uptimeText = computed(() => fmtUptime(metrics.value?.uptimeSec));

// ── 历史序列 / 峰值 ─────────────────────────────────────────────────────
const lastPoint = computed(() =>
  points.value.length > 0 ? points.value.at(-1) : undefined,
);
const cpuSeries = computed(() =>
  points.value.map((p) => ({ ts: p.ts ?? '', value: Number(p.cpuPct ?? 0) })),
);
const memSeries = computed(() =>
  points.value.map((p) => ({ ts: p.ts ?? '', value: Number(p.memPct ?? 0) })),
);
const diskSeries = computed(() =>
  points.value.map((p) => ({
    ts: p.ts ?? '',
    value: Number(p.diskMaxPct ?? 0),
  })),
);
const loadSeries = computed(() =>
  points.value.map((p) => ({ ts: p.ts ?? '', value: Number(p.load1 ?? 0) })),
);
const timeFormat = computed(() =>
  range.value === '1h' || range.value === '6h' || range.value === '24h'
    ? 'HH:mm'
    : 'MM-DD',
);

// ── 饼图数据（来自当前快照）────────────────────────────────────────────
const diskItems = computed(() =>
  (metrics.value?.disks ?? []).map((d) => ({
    label: d.mount || '—',
    value: Number(d.usedBytes ?? 0),
  })),
);
const diskUsedTotal = computed(() =>
  (metrics.value?.disks ?? []).reduce((sum, d) => sum + (d.usedBytes ?? 0), 0),
);
const memItems = computed(() => [
  {
    label: $t('logs.host.pie.memUsed'),
    value: Number(metrics.value?.memory?.usedBytes ?? 0),
  },
  {
    label: $t('logs.host.pie.memAvailable'),
    value: Number(metrics.value?.memory?.availableBytes ?? 0),
  },
]);
const poolItems = computed(() => {
  const used = Number(lastPoint.value?.dbPoolInUse ?? 0);
  const max = Number(lastPoint.value?.dbPoolMax ?? 0);
  return [
    { label: $t('logs.host.pie.poolUsed'), value: used },
    { label: $t('logs.host.pie.poolFree'), value: Math.max(0, max - used) },
  ];
});

// ── 查询 ────────────────────────────────────────────────────────────────
function stepFor(from: string, to: string): number {
  const spanMinutes = Math.max(1, (Date.parse(to) - Date.parse(from)) / 60_000);
  if (spanMinutes <= 360) return 60;
  if (spanMinutes <= 1440) return 120;
  if (spanMinutes <= 10_080) return 600;
  return 1800;
}

async function loadHistory() {
  const { from, to } = windowRange.value;
  const fromMs = Date.parse(from);
  const toMs = Date.parse(to);
  if (!(fromMs < toMs)) {
    message.warning($t('logs.host.messages.invalidRange'));
    return;
  }
  loading.value = true;
  const hours = Math.min(
    2160,
    Math.max(1, Math.ceil((toMs - fromMs) / 3_600_000)),
  );
  const [hist, sum] = await Promise.allSettled([
    getSystemMetricsHistory({ from, step: stepFor(from, to), to }),
    getSystemMetricsSummary(hours),
  ]);
  if (hist.status === 'fulfilled') {
    points.value = hist.value.items ?? [];
  } else {
    points.value = [];
    message.error($t('logs.host.messages.queryFail'));
  }
  summary.value = sum.status === 'fulfilled' ? sum.value : undefined;
  lastUpdated.value = new Date().toLocaleTimeString(undefined, {
    hour12: false,
  });
  loading.value = false;
}

async function loadSnapshot() {
  snapshotLoading.value = true;
  try {
    metrics.value = await getSystemMetrics();
  } catch {
    // 快照失败保留上一次值，避免页面闪空；历史与查询不受影响。
  } finally {
    snapshotLoading.value = false;
  }
}

function applyPreset(key: RangeKey) {
  const conf = RANGE_CONF[key];
  const to = new Date();
  const from = new Date(to.getTime() - conf.hours * 3_600_000);
  range.value = key;
  customRange.value = undefined;
  windowRange.value = { from: from.toISOString(), to: to.toISOString() };
  void loadHistory();
}

function onRangeChange(value: unknown) {
  const pair = value as [Dayjs, Dayjs] | null | undefined;
  if (!pair || !pair[0] || !pair[1]) return;
  range.value = 'custom';
  customRange.value = [pair[0], pair[1]];
  windowRange.value = {
    from: pair[0].toISOString(),
    to: pair[1].toISOString(),
  };
  void loadHistory();
}

function runQuery() {
  if (range.value === 'custom' && customRange.value) {
    windowRange.value = {
      from: customRange.value[0].toISOString(),
      to: customRange.value[1].toISOString(),
    };
  }
  void loadHistory();
}

function refreshAll() {
  void loadSnapshot();
  void loadHistory();
}

// ── 明细表 ──────────────────────────────────────────────────────────────
const rows = computed(() =>
  [...points.value].toSorted(
    (a, b) => Date.parse(b.ts ?? '') - Date.parse(a.ts ?? ''),
  ),
);
const columns = computed(() => [
  { dataIndex: 'ts', key: 'ts', title: $t('logs.host.table.time'), width: 168 },
  {
    dataIndex: 'cpuPct',
    key: 'cpu',
    title: $t('logs.host.table.cpu'),
    width: 84,
  },
  {
    dataIndex: 'memPct',
    key: 'mem',
    title: $t('logs.host.table.memory'),
    width: 84,
  },
  {
    dataIndex: 'diskMaxPct',
    key: 'disk',
    title: $t('logs.host.table.disk'),
    width: 84,
  },
  {
    dataIndex: 'load1',
    key: 'load',
    title: $t('logs.host.table.load'),
    width: 84,
  },
  {
    dataIndex: 'goroutines',
    key: 'goroutines',
    title: $t('logs.host.table.goroutines'),
    width: 92,
  },
  {
    dataIndex: 'dbPoolInUse',
    key: 'pool',
    title: $t('logs.host.table.pool'),
    width: 108,
  },
  {
    dataIndex: 'logLatestAgeSec',
    key: 'logAge',
    title: $t('logs.host.table.logAge'),
    width: 96,
  },
]);

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  applyPreset('24h');
  void loadSnapshot();
  timer = setInterval(() => void loadSnapshot(), 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:server"
      :title="$t('logs.host.title')"
      :description="$t('logs.host.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          {{ $t('logs.host.uptimeLine', { uptime: uptimeText }) }}
          ·
          {{ $t('logs.host.updatedAt', { time: lastUpdated || '—' }) }}
        </span>
        <Button
          size="small"
          :loading="loading || snapshotLoading"
          @click="refreshAll"
        >
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('logs.common.refresh')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('logs.host.kpi.cpu')"
        :value="metrics ? fmtPct(cpuPct) : '…'"
        icon="lucide:cpu"
        tone="blue"
        :progress="cpuPct"
        :hint="$t('logs.host.kpi.cpuHint', { n: metrics?.cpu?.cores ?? 0 })"
      />
      <StatCard
        :label="$t('logs.host.kpi.memory')"
        :value="metrics ? fmtPct(memPct) : '…'"
        icon="lucide:memory-stick"
        tone="cyan"
        :progress="memPct"
        :hint="
          $t('logs.host.kpi.memoryHint', {
            total: fmtBytes(metrics?.memory?.totalBytes),
            used: fmtBytes(metrics?.memory?.usedBytes),
          })
        "
      />
      <StatCard
        :label="$t('logs.host.kpi.disk')"
        :value="metrics ? fmtPct(diskPct) : '…'"
        icon="lucide:hard-drive"
        tone="amber"
        :progress="diskPct"
        :hint="
          $t('logs.host.kpi.diskHint', {
            mount: worstDisk?.mount ?? '—',
          })
        "
      />
      <StatCard
        :label="$t('logs.host.kpi.load')"
        :value="metrics ? load1.toFixed(2) : '…'"
        icon="lucide:activity"
        tone="violet"
        :hint="
          $t('logs.host.kpi.loadHint', {
            l15: Number(metrics?.cpu?.load15 ?? 0).toFixed(2),
            l5: Number(metrics?.cpu?.load5 ?? 0).toFixed(2),
          })
        "
      />
    </div>

    <Card>
      <div class="ipam-toolbar ipam-toolbar--plain">
        <div class="ipam-seg">
          <button
            v-for="r in ranges"
            :key="r.key"
            type="button"
            :data-active="range === r.key"
            @click="applyPreset(r.key)"
          >
            {{ r.label }}
          </button>
        </div>
        <DatePicker.RangePicker
          :value="customRange"
          :allow-clear="true"
          format="YYYY-MM-DD HH:mm:ss"
          :placeholder="[
            $t('logs.common.startTime'),
            $t('logs.common.endTime'),
          ]"
          :show-time="{ format: 'HH:mm:ss' }"
          style="width: 340px"
          @change="onRangeChange"
        />
        <Button
          type="primary"
          size="small"
          :loading="loading"
          @click="runQuery"
        >
          <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />{{
            $t('logs.common.query')
          }}
        </Button>
        <Button size="small" @click="applyPreset('24h')">
          {{ $t('logs.common.reset') }}
        </Button>
        <span class="ipam-toolbar__spacer"></span>
        <Tag class="m-0" color="blue">{{ windowLabel }}</Tag>
        <span class="ipam-hint">{{ $t('logs.host.rangeHint') }}</span>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:chart-line" />{{
            $t('logs.host.trend.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('logs.host.trend.hint') }}</span>
      </template>

      <template v-if="points.length > 0">
        <div class="mb-3 flex flex-wrap items-center gap-3">
          <span class="ipam-hint">
            <IconifyIcon
              icon="lucide:trending-up"
              class="mr-1 inline size-3.5"
            />
            {{ $t('logs.host.peak.cpu', { v: fmtPct(summary?.cpuMax) }) }}
            · {{ $t('logs.host.peak.mem', { v: fmtPct(summary?.memMax) }) }}
            ·
            {{
              $t('logs.host.peak.disk', {
                mount: summary?.diskMaxMount ?? '—',
                v: fmtPct(summary?.diskMax),
              })
            }}
            ·
            {{
              $t('logs.host.peak.load', {
                v: Number(summary?.load1Max ?? 0).toFixed(2),
              })
            }}
          </span>
          <span v-if="summary?.since" class="ipam-hint">
            {{
              $t('logs.host.peak.since', {
                time: fmtDateTime(summary.since),
              })
            }}
          </span>
        </div>

        <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
          <div>
            <div class="flex items-baseline justify-between">
              <span class="ipam-figure__label">{{
                $t('logs.host.trend.cpu')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtPct(lastPoint?.cpuPct)
              }}</span>
            </div>
            <TrendChart
              :data="cpuSeries"
              :height="150"
              :markers="false"
              :time-format="timeFormat"
              tone="blue"
              unit="%"
            />
          </div>
          <div>
            <div class="flex items-baseline justify-between">
              <span class="ipam-figure__label">{{
                $t('logs.host.trend.memory')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtPct(lastPoint?.memPct)
              }}</span>
            </div>
            <TrendChart
              :data="memSeries"
              :height="150"
              :markers="false"
              :time-format="timeFormat"
              tone="cyan"
              unit="%"
            />
          </div>
          <div>
            <div class="flex items-baseline justify-between">
              <span class="ipam-figure__label">{{
                $t('logs.host.trend.disk')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtPct(lastPoint?.diskMaxPct)
              }}</span>
            </div>
            <TrendChart
              :data="diskSeries"
              :height="150"
              :markers="false"
              :time-format="timeFormat"
              tone="amber"
              unit="%"
            />
            <span class="ipam-form-hint">
              {{
                $t('logs.host.trend.diskHint', {
                  mount: lastPoint?.diskWorstMount ?? '—',
                })
              }}
            </span>
          </div>
          <div>
            <div class="flex items-baseline justify-between">
              <span class="ipam-figure__label">{{
                $t('logs.host.trend.load')
              }}</span>
              <span class="ipam-figure__value">{{
                Number(lastPoint?.load1 ?? 0).toFixed(2)
              }}</span>
            </div>
            <TrendChart
              :data="loadSeries"
              :height="150"
              :markers="false"
              :time-format="timeFormat"
              tone="violet"
            />
          </div>
        </div>
      </template>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__icon">
          <IconifyIcon icon="lucide:chart-line" class="size-5" />
        </div>
        <div class="ipam-empty__title">
          {{ $t('logs.host.trend.emptyTitle') }}
        </div>
        <div class="ipam-empty__desc">
          {{ $t('logs.host.trend.emptyDesc') }}
        </div>
      </div>
    </Card>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:pie-chart" />{{
              $t('logs.host.pie.diskTitle')
            }}
          </span>
        </template>
        <PieChart
          :center-caption="$t('logs.host.pie.diskCaption')"
          :center-value="fmtBytes(diskUsedTotal)"
          :empty-text="$t('logs.host.pie.empty')"
          :formatter="fmtBytes"
          :items="diskItems"
        />
      </Card>
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:pie-chart" />{{
              $t('logs.host.pie.memTitle')
            }}
          </span>
        </template>
        <PieChart
          :center-caption="$t('logs.host.pie.memCaption')"
          :center-value="fmtPct(metrics?.memory?.usedPct)"
          :empty-text="$t('logs.host.pie.empty')"
          :formatter="fmtBytes"
          :items="memItems"
        />
      </Card>
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:pie-chart" />{{
              $t('logs.host.pie.poolTitle')
            }}
          </span>
        </template>
        <PieChart
          :center-caption="$t('logs.host.pie.poolCaption')"
          :center-value="`${lastPoint?.dbPoolInUse ?? 0} / ${lastPoint?.dbPoolMax ?? 0}`"
          :empty-text="$t('logs.host.pie.empty')"
          :formatter="(v: number) => String(v)"
          :items="poolItems"
        />
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:table-2" />{{ $t('logs.host.table.title') }}
        </span>
      </template>
      <template #extra>
        <Tag class="m-0">{{
          $t('logs.host.table.count', { n: rows.length })
        }}</Tag>
      </template>

      <Table
        :columns="columns"
        :data-source="rows"
        :locale="{ emptyText: $t('logs.host.table.empty') }"
        :pagination="{ pageSize: 10, showSizeChanger: false }"
        :row-key="(r: SystemMetricPoint) => String(r.ts)"
        :scroll="{ x: 880 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'ts'">
            <span class="ipam-log-time">
              <b>{{ fmtTime(record.ts) }}</b>
              <span>{{ fmtDateTime(record.ts) }}</span>
            </span>
          </template>
          <template v-else-if="column.key === 'cpu'">
            {{ fmtPct(record.cpuPct) }}
          </template>
          <template v-else-if="column.key === 'mem'">
            {{ fmtPct(record.memPct) }}
          </template>
          <template v-else-if="column.key === 'disk'">
            {{ fmtPct(record.diskMaxPct) }}
          </template>
          <template v-else-if="column.key === 'load'">
            {{ Number(record.load1 ?? 0).toFixed(2) }}
          </template>
          <template v-else-if="column.key === 'goroutines'">
            {{ record.goroutines ?? '—' }}
          </template>
          <template v-else-if="column.key === 'pool'">
            {{ record.dbPoolInUse ?? 0 }} / {{ record.dbPoolMax ?? 0 }}
          </template>
          <template v-else-if="column.key === 'logAge'">
            {{
              record.logLatestAgeSec === undefined ||
              record.logLatestAgeSec === null ||
              record.logLatestAgeSec < 0
                ? '—'
                : $t('logs.host.table.logAgeValue', {
                    n: record.logLatestAgeSec,
                  })
            }}
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>
