<script setup lang="ts">
import type { DashboardOverview, HostMetrics } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Skeleton } from 'ant-design-vue';
import dayjs from 'dayjs';

import {
  getDashboard,
  getLogQps,
  getSystemMetrics,
  listLogTop,
} from '#/api/ipam';
import MetricRing from '#/components/metric-ring.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';
import {
  readOnboarding,
  useOnboardingUsername,
  writeOnboarding,
} from '#/composables/use-onboarding';
import { $t } from '#/locales';

type Light = 'down' | 'unknown' | 'up';
const LIGHT_TEXT_KEY: Record<Light, string> = {
  up: 'dashboard.ipam.light.up',
  down: 'dashboard.ipam.light.down',
  unknown: 'dashboard.ipam.light.unknown',
};
const LIGHT_LABEL: Record<string, string> = {
  postgres: 'PostgreSQL',
  clickhouse: 'ClickHouse',
  kea: 'Kea DHCP',
  unbound: 'Unbound DNS',
};

const router = useRouter();
const onboardingName = useOnboardingUsername();
const onboardingState = ref(readOnboarding(onboardingName));
const showOnboarding = computed(
  () =>
    !onboardingState.value.completed && !onboardingState.value.dismissed,
);
function dismissOnboarding() {
  onboardingState.value = writeOnboarding(onboardingName, { dismissed: true });
}

const data = ref<DashboardOverview>();
const metrics = ref<HostMetrics>();
const loading = ref(false);
const lastUpdated = ref<Date>();
let timer: ReturnType<typeof setInterval> | undefined;
let metricsTimer: ReturnType<typeof setInterval> | undefined;

// ── 数据加载 ──
async function loadDashboard() {
  data.value = await getDashboard(5);
  lastUpdated.value = new Date();
  void loadQps();
  void loadTopDomains();
}
async function loadMetrics() {
  try {
    metrics.value = await getSystemMetrics();
  } catch {
    metrics.value = undefined;
  }
}
async function refreshAll() {
  loading.value = true;
  try {
    await Promise.all([loadDashboard(), loadMetrics()]);
  } finally {
    loading.value = false;
  }
}
onMounted(() => {
  void refreshAll();
  timer = setInterval(() => void loadDashboard(), 30_000);
  metricsTimer = setInterval(() => void loadMetrics(), 10_000);
});
onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
  if (metricsTimer) clearInterval(metricsTimer);
});

// ── 格式化 ──
function fmtInt(v?: null | number): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return v.toLocaleString('zh-CN');
}
function fmtDecimal(v?: null | number, digits = 1): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return Number(v).toFixed(digits);
}
function fmtPct(v?: null | number, digits = 1): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${Number(v).toFixed(digits)}%`;
}
function fmtBytes(v?: null | number): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let n = v;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n >= 10 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}
function fmtUptime(sec?: number): string {
  if (!sec) return '—';
  const d = Math.floor(sec / 86_400);
  const h = Math.floor((sec % 86_400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return d > 0
    ? $t('dashboard.ipam.host.uptimeDays', { d, h })
    : $t('dashboard.ipam.host.uptimeHours', { h, m });
}
function toneOfPct(pct: number): 'amber' | 'green' | 'red' {
  if (pct > 90) return 'red';
  if (pct > 75) return 'amber';
  return 'green';
}

// ── 活跃终端趋势 ──
const onlineSeries = computed(() => {
  const list = data.value?.onlineTrend ?? [];
  // 末尾小时为「进行中」的不完整桶，直接剔除避免趋势尾部出现假下跌
  const now = Date.now();
  const full = list.filter((p) => new Date(p.ts).getTime() + 3_600_000 <= now);
  const source = full.length > 0 ? full : list;
  return source.map((p) => ({ ts: p.ts, value: p.count }));
});
const onlineSpark = computed(() => onlineSeries.value.map((p) => p.value));
const onlinePeak = computed(() => Math.max(0, ...onlineSpark.value));
const onlineAvg = computed(() => {
  const n = onlineSpark.value.length;
  return n === 0
    ? 0
    : Math.round(onlineSpark.value.reduce((sum, v) => sum + v, 0) / n);
});

// ── DNS 查询量 ──
const RANGES = computed(() => [
  {
    hours: 1 as const,
    intervalSec: 60,
    label: $t('dashboard.ipam.qps.range1h'),
  },
  {
    hours: 6 as const,
    intervalSec: 300,
    label: $t('dashboard.ipam.qps.range6h'),
  },
  {
    hours: 24 as const,
    intervalSec: 900,
    label: $t('dashboard.ipam.qps.range24h'),
  },
]);
type RangeHours = 1 | 6 | 24;

const qpsRange = ref<RangeHours>(1);
const qpsPoints = ref<{ count: number; ts: string }[]>([]);
const currentRange = computed(
  () => RANGES.value.find((r) => r.hours === qpsRange.value) ?? RANGES.value[0]!,
);
const qpsBucketMin = computed(() => currentRange.value.intervalSec / 60);
const qpsSeries = computed(() =>
  qpsPoints.value.map((p) => ({ ts: p.ts, value: p.count })),
);
const qpsSpark = computed(() => qpsSeries.value.slice(-30).map((p) => p.value));
const qpsTotal = computed(() =>
  qpsPoints.value.reduce((sum, p) => sum + p.count, 0),
);
const qpsAvgRate = computed(() => {
  const minutes = currentRange.value.hours * 60;
  return minutes > 0 ? qpsTotal.value / minutes : 0;
});
const qpsPeakRate = computed(() => {
  const peak = Math.max(0, ...qpsPoints.value.map((p) => p.count));
  return peak / qpsBucketMin.value;
});

let qpsSeq = 0;
async function loadQps() {
  const seq = (qpsSeq += 1);
  const now = new Date();
  const from = new Date(now.getTime() - currentRange.value.hours * 3_600_000);
  try {
    const res = await getLogQps({
      from: from.toISOString(),
      to: now.toISOString(),
      intervalSec: currentRange.value.intervalSec,
    });
    if (seq !== qpsSeq) return;
    const list = res.points ?? [];
    // 剔除仍在进行中的末尾分桶，避免趋势尾部跌到 0
    const bucketMs = currentRange.value.intervalSec * 1000;
    const nowMs = Date.now();
    const full = list.filter((p) => new Date(p.ts).getTime() + bucketMs <= nowMs);
    qpsPoints.value = full.length > 0 ? full : list;
  } catch {
    if (seq === qpsSeq) qpsPoints.value = [];
  }
}
function setRange(hours: RangeHours) {
  if (qpsRange.value === hours) return;
  qpsRange.value = hours;
  void loadQps();
}

// ── 热门查询域名 ──
const topDomains = ref<{ count: number; key: string }[]>([]);
const topDomainMax = computed(() =>
  Math.max(1, ...topDomains.value.map((d) => d.count)),
);
async function loadTopDomains() {
  const now = new Date();
  try {
    const res = await listLogTop({
      from: new Date(now.getTime() - 3_600_000).toISOString(),
      to: now.toISOString(),
      by: 'domain',
      limit: 5,
    });
    topDomains.value = (res.items ?? []).slice(0, 5);
  } catch {
    topDomains.value = [];
  }
}

// ── 服务健康 ──
const healthServices = computed(() =>
  Object.entries(LIGHT_LABEL).map(([key, label]) => {
    const state = (data.value?.services?.[
      key as keyof DashboardOverview['services']
    ] ?? 'unknown') as Light;
    return { key, label, state, text: $t(LIGHT_TEXT_KEY[state]) };
  }),
);
const healthUp = computed(
  () => healthServices.value.filter((s) => s.state === 'up').length,
);
const healthTotal = computed(
  () => healthServices.value.filter((s) => s.state !== 'unknown').length,
);
const healthRatio = computed(() =>
  healthTotal.value ? (healthUp.value / healthTotal.value) * 100 : 0,
);
const healthTone = computed<'amber' | 'green' | 'red'>(() => {
  if (healthServices.value.some((s) => s.state === 'down')) return 'red';
  if (healthUp.value === 0) return 'amber';
  return 'green';
});
const healthHint = computed(() => {
  const down = healthServices.value.filter((s) => s.state === 'down');
  if (down.length > 0) {
    return $t('dashboard.ipam.health.hintDown', {
      names: down.map((s) => s.label).join($t('dashboard.ipam.listSep')),
    });
  }
  return healthServices.value.some((s) => s.state === 'unknown')
    ? $t('dashboard.ipam.health.hintPartial')
    : $t('dashboard.ipam.health.hintAll');
});

// ── 主机资源 ──
const cpuPct = computed(() => metrics.value?.cpu?.usagePct ?? 0);
const memPct = computed(() => metrics.value?.memory?.usedPct ?? 0);
const rootDisk = computed(
  () => metrics.value?.disks?.find((d) => d.mount === '/') ?? metrics.value?.disks?.[0],
);
const diskPct = computed(() => Number(rootDisk.value?.usedPct ?? 0));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Dashboard"
      icon="lucide:layout-dashboard"
      :title="$t('dashboard.ipam.header.title')"
      :description="$t('dashboard.ipam.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock-3" class="size-3.5" />
          {{
            lastUpdated
              ? $t('dashboard.ipam.header.updatedAt', {
                  time: dayjs(lastUpdated).format('HH:mm:ss'),
                })
              : $t('dashboard.ipam.header.waiting')
          }}
        </span>
        <Button :loading="loading" @click="refreshAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          {{ $t('dashboard.ipam.header.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <!-- 首次使用向导入口（M6-024） -->
    <div
      v-if="showOnboarding"
      class="ipam-note mb-4 flex-wrap items-center gap-3"
    >
      <IconifyIcon icon="lucide:rocket" class="size-4 shrink-0" />
      <div class="min-w-[220px] flex-1">
        <div class="font-medium">{{ $t('page.onboarding.title') }}</div>
        <div class="ipam-hint">{{ $t('page.onboarding.bannerDesc') }}</div>
      </div>
      <Button size="small" type="primary" @click="router.push('/onboarding')">
        {{ $t('page.onboarding.bannerStart') }}
      </Button>
      <Button size="small" @click="dismissOnboarding">
        {{ $t('page.onboarding.dismiss') }}
      </Button>
    </div>

    <!-- 核心指标 -->
    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dashboard.ipam.stat.activeDevices')"
        :value="fmtInt(data?.onlineNow)"
        :unit="$t('dashboard.ipam.unit.devices')"
        icon="lucide:monitor-smartphone"
        tone="blue"
        :spark="onlineSpark"
        :hint="
          $t('dashboard.ipam.stat.activeHint', {
            new: fmtInt(data?.newTerminals),
            offline: fmtInt(data?.offlineTerminals),
          })
        "
      />
      <StatCard
        :label="$t('dashboard.ipam.stat.qps')"
        :value="fmtDecimal(data?.dns?.qps5m)"
        unit="QPS"
        icon="lucide:activity"
        tone="cyan"
        :spark="qpsSpark"
        :hint="$t('dashboard.ipam.stat.qpsHint', { avg: fmtDecimal(qpsAvgRate) })"
      />
      <StatCard
        :label="$t('dashboard.ipam.stat.health')"
        :value="`${healthUp} / ${healthTotal || '—'}`"
        icon="lucide:shield-check"
        :tone="healthTone"
        :hint="healthHint"
        :progress="healthRatio"
      />
      <StatCard
        :label="$t('dashboard.ipam.stat.blocked')"
        :value="fmtInt(data?.dns?.blockedToday ?? null)"
        :unit="$t('dashboard.ipam.unit.times')"
        icon="lucide:shield-ban"
        tone="violet"
        :hint="$t('dashboard.ipam.stat.blockedHint')"
      />
    </div>

    <!-- 活跃终端趋势 + 服务健康 -->
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:users" />
            {{ $t('dashboard.ipam.online.title') }}
            <em class="ipam-card-sub">{{ $t('dashboard.ipam.online.sub') }}</em>
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">
            {{ $t('dashboard.ipam.online.peak', { n: fmtInt(onlinePeak) }) }}
          </span>
        </template>

        <Skeleton v-if="!data" active :paragraph="{ rows: 4 }" />
        <template v-else-if="onlineSeries.length > 0">
          <TrendChart
            :data="onlineSeries"
            :height="196"
            :markers="false"
            bars
            tone="blue"
            :unit="$t('dashboard.ipam.unit.devices')"
          />
          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.online.new') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(data?.newTerminals) }}</span>
              <span class="ipam-figure__unit">{{ $t('dashboard.ipam.unit.devices') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.online.offline') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(data?.offlineTerminals) }}</span>
              <span class="ipam-figure__unit">{{ $t('dashboard.ipam.unit.devices') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.online.hourlyAvg') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(onlineAvg) }}</span>
              <span class="ipam-figure__unit">{{ $t('dashboard.ipam.unit.devices') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.online.dualstack') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(data?.coherenceSuccessRatePct) }}</span>
              <span class="ipam-figure__unit">%</span>
              <span class="ipam-figure__hint">DHCP ↔ DNS</span>
            </div>
          </div>
        </template>
        <div v-else class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:bar-chart-3" class="size-5" />
          </div>
          <div class="ipam-empty__title">{{ $t('dashboard.ipam.online.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('dashboard.ipam.online.emptyDesc') }}</div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:shield-check" />
            {{ $t('dashboard.ipam.stat.health') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-chip" :data-tone="healthTone">
            {{
              healthTone === 'green'
                ? $t('dashboard.ipam.health.ok')
                : healthTone === 'red'
                  ? $t('dashboard.ipam.health.bad')
                  : $t('dashboard.ipam.health.pending')
            }}
          </span>
        </template>

        <div class="ipam-health-summary">
          <MetricRing
            :value="healthRatio"
            :size="112"
            :thickness="9"
            :tone="healthTone"
            :label="`${healthUp}/${healthTotal || 0}`"
            :caption="$t('dashboard.ipam.health.captionOk')"
            :empty="healthTotal === 0"
          />
          <div class="ipam-health-summary__text">
            <div class="ipam-health-summary__title">{{ healthHint }}</div>
            <div class="ipam-hint">{{ $t('dashboard.ipam.health.probeHint') }}</div>
          </div>
        </div>

        <div class="ipam-health-grid">
          <div
            v-for="s in healthServices"
            :key="s.key"
            class="ipam-health-item"
            :data-state="s.state"
          >
            <span class="ipam-health-item__dot"></span>
            <div class="min-w-0">
              <div class="ipam-health-item__name">{{ s.label }}</div>
              <div class="ipam-hint">{{ s.text }}</div>
            </div>
          </div>
        </div>

        <div class="ipam-note mt-3">
          <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
          <span>{{ $t('dashboard.ipam.health.poc') }}</span>
        </div>
      </Card>
    </div>

    <!-- DNS 查询量 + 热门域名 -->
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:activity" />
            {{ $t('dashboard.ipam.qps.title') }}
            <em class="ipam-card-sub">
              {{ $t('dashboard.ipam.qps.bucket', { min: qpsBucketMin }) }}
            </em>
          </span>
        </template>
        <template #extra>
          <div class="ipam-card-tools">
            <span class="ipam-card-legend">
              <span><i data-kind="peak"></i>{{ $t('dashboard.ipam.qps.peak') }}</span>
              <span><i data-kind="avg"></i>{{ $t('dashboard.ipam.qps.avg') }}</span>
            </span>
            <div class="ipam-seg">
              <button
                v-for="r in RANGES"
                :key="r.hours"
                :data-active="qpsRange === r.hours"
                type="button"
                @click="setRange(r.hours)"
              >
                {{ r.label }}
              </button>
            </div>
          </div>
        </template>

        <Skeleton v-if="!data" active :paragraph="{ rows: 4 }" />
        <template v-else>
          <TrendChart
            v-if="qpsSeries.length > 0"
            :data="qpsSeries"
            :height="188"
            :time-format="qpsRange === 24 ? 'MM-DD HH:mm' : 'HH:mm'"
            tone="cyan"
            :unit="$t('dashboard.ipam.unit.times')"
          />
          <div v-else class="ipam-empty">
            <div class="ipam-empty__icon">
              <IconifyIcon icon="lucide:activity" class="size-5" />
            </div>
            <div class="ipam-empty__title">{{ $t('dashboard.ipam.qps.emptyTitle') }}</div>
            <div class="ipam-empty__desc">{{ $t('dashboard.ipam.qps.emptyDesc') }}</div>
          </div>
          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.qps.avgRate') }}</span>
              <span class="ipam-figure__value">{{ fmtDecimal(qpsAvgRate) }}</span>
              <span class="ipam-figure__unit">{{ $t('dashboard.ipam.unit.perMin') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.qps.peakRate') }}</span>
              <span class="ipam-figure__value">{{ fmtDecimal(qpsPeakRate) }}</span>
              <span class="ipam-figure__unit">{{ $t('dashboard.ipam.unit.perMin') }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.qps.total') }}</span>
              <span class="ipam-figure__value">{{ fmtInt(qpsTotal) }}</span>
              <span class="ipam-figure__unit">{{ $t('dashboard.ipam.unit.times') }}</span>
            </div>
          </div>
        </template>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:flame" />
            {{ $t('dashboard.ipam.top.title') }}
            <em class="ipam-card-sub">{{ $t('dashboard.ipam.top.sub') }}</em>
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/logs-center/search">
            {{ $t('dashboard.ipam.top.details') }}
            <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
          </RouterLink>
        </template>

        <div v-if="topDomains.length > 0" class="ipam-rank">
          <div
            v-for="(d, i) in topDomains"
            :key="d.key"
            class="ipam-rank__item"
          >
            <span class="ipam-rank__no" :data-top="i < 3">{{ i + 1 }}</span>
            <div class="ipam-rank__body">
              <div class="ipam-rank__title">
                <span class="ipam-rank__name" :title="d.key">{{ d.key }}</span>
                <span class="ipam-rank__value">{{ fmtInt(d.count) }}</span>
              </div>
              <div class="ipam-progress ipam-rank__bar">
                <i :style="{ width: `${Math.max(4, (d.count / topDomainMax) * 100)}%` }"></i>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:flame" class="size-5" />
          </div>
          <div class="ipam-empty__title">{{ $t('dashboard.ipam.top.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('dashboard.ipam.top.emptyDesc') }}</div>
        </div>
      </Card>
    </div>

    <!-- 主机资源 + 地址池利用率 -->
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:server" />
            {{ $t('dashboard.ipam.host.title') }}
            <em class="ipam-card-sub">{{ $t('dashboard.ipam.host.sub') }}</em>
          </span>
        </template>
        <template #extra>
          <span v-if="metrics" class="ipam-hint">
            {{
              $t('dashboard.ipam.host.sampled', {
                time: dayjs(metrics.ts).format('HH:mm:ss'),
                uptime: fmtUptime(metrics.uptimeSec),
              })
            }}
          </span>
        </template>

        <Skeleton v-if="!metrics" active :paragraph="{ rows: 3 }" />
        <template v-else>
          <div class="ipam-gauges">
            <div class="ipam-gauge">
              <MetricRing
                :value="cpuPct"
                :size="96"
                :thickness="8"
                :tone="toneOfPct(cpuPct)"
                :label="fmtPct(cpuPct)"
              />
              <div class="ipam-gauge__caption">
                <span>CPU</span>
                <small>
                  {{ $t('dashboard.ipam.host.cores', { n: metrics.cpu.cores }) }}
                </small>
              </div>
            </div>
            <div class="ipam-gauge">
              <MetricRing
                :value="memPct"
                :size="96"
                :thickness="8"
                :tone="toneOfPct(memPct)"
                :label="fmtPct(memPct)"
              />
              <div class="ipam-gauge__caption">
                <span>{{ $t('dashboard.ipam.host.memory') }}</span>
                <small>
                  {{ fmtBytes(metrics.memory.usedBytes) }} / {{ fmtBytes(metrics.memory.totalBytes) }}
                </small>
              </div>
            </div>
            <div class="ipam-gauge">
              <MetricRing
                :value="diskPct"
                :size="96"
                :thickness="8"
                :tone="toneOfPct(diskPct)"
                :label="fmtPct(diskPct)"
                :empty="!rootDisk"
              />
              <div class="ipam-gauge__caption">
                <span>{{ $t('dashboard.ipam.host.rootfs') }}</span>
                <small>
                  {{ fmtBytes(rootDisk?.usedBytes) }} / {{ fmtBytes(rootDisk?.totalBytes) }}
                </small>
              </div>
            </div>
          </div>

          <div class="ipam-disk-list">
            <div v-for="d in metrics.disks" :key="d.mount" class="ipam-disk">
              <div class="ipam-disk__head">
                <span class="ipam-disk__mount">{{ d.mount }}</span>
                <span class="ipam-disk__meta">{{ d.device }} · {{ d.fsType }}</span>
                <span class="ipam-disk__size">
                  {{ fmtBytes(d.usedBytes) }} / {{ fmtBytes(d.totalBytes) }}
                </span>
                <span class="ipam-disk__pct">{{ fmtPct(d.usedPct) }}</span>
              </div>
              <div class="ipam-progress" :data-tone="toneOfPct(Number(d.usedPct) || 0)">
                <i :style="{ width: `${Math.min(100, Number(d.usedPct) || 0)}%` }"></i>
              </div>
            </div>
          </div>
          <div v-if="metrics.disks.length === 0" class="ipam-empty">
            <div class="ipam-empty__icon">
              <IconifyIcon icon="lucide:hard-drive" class="size-5" />
            </div>
            <div class="ipam-empty__title">{{ $t('dashboard.ipam.host.emptyTitle') }}</div>
            <div class="ipam-empty__desc">{{ $t('dashboard.ipam.host.emptyDesc') }}</div>
          </div>

          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.host.load1') }}</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load1, 2) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.host.load5') }}</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load5, 2) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.host.load15') }}</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load15, 2) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{ $t('dashboard.ipam.host.uptime') }}</span>
              <span class="ipam-figure__value">{{ fmtUptime(metrics.uptimeSec) }}</span>
            </div>
          </div>
        </template>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:network" />
            {{ $t('dashboard.ipam.pools.title') }}
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/dhcp/subnets">
            {{ $t('dashboard.ipam.pools.manage') }}
            <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
          </RouterLink>
        </template>

        <div v-if="(data?.poolUtilTop ?? []).length > 0" class="ipam-rank">
          <div
            v-for="(p, i) in data?.poolUtilTop ?? []"
            :key="`${p.subnetId}-${p.poolIndex}`"
            class="ipam-rank__item ipam-rank__item--pool"
          >
            <span class="ipam-rank__no" :data-top="i < 3">{{ i + 1 }}</span>
            <div class="ipam-rank__body">
              <div class="ipam-rank__title">
                <span class="ipam-rank__name" :title="p.name">{{ p.name }}</span>
                <span class="ipam-rank__value">{{ fmtPct(p.pct) }}</span>
              </div>
              <div class="ipam-rank__tags">
                <span class="ipam-rank__cidr ipam-mono">{{ p.cidr }}</span>
                <span
                  v-if="p.orgName"
                  class="ipam-rank__dept"
                  :title="$t('dashboard.ipam.pools.dept', { name: p.orgName })"
                >
                  <IconifyIcon icon="lucide:building-2" class="size-3" />{{ p.orgName }}
                </span>
              </div>
              <div
                class="ipam-progress"
                :data-tone="toneOfPct(Number(p.pct) || 0)"
              >
                <i :style="{ width: `${Math.min(100, Number(p.pct) || 0)}%` }"></i>
              </div>
              <div class="ipam-rank__meta">
                {{
                  $t('dashboard.ipam.pools.used', {
                    used: fmtInt(p.used),
                    capacity: fmtInt(p.capacity),
                  })
                }}
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:network" class="size-5" />
          </div>
          <div class="ipam-empty__title">{{ $t('dashboard.ipam.pools.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('dashboard.ipam.pools.emptyDesc') }}</div>
        </div>
      </Card>
    </div>
  </div>
</template>
