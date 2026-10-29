<script setup lang="ts">
import type { DashboardOverview, HostMetrics } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

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

type Light = 'down' | 'unknown' | 'up';
const LIGHT_TEXT: Record<Light, string> = {
  up: '运行正常',
  down: '服务异常',
  unknown: '未接入',
};
const LIGHT_LABEL: Record<string, string> = {
  postgres: 'PostgreSQL',
  clickhouse: 'ClickHouse',
  kea: 'Kea DHCP',
  unbound: 'Unbound DNS',
};

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
  return d > 0 ? `${d}天 ${h}时` : `${h}时 ${m}分`;
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
const RANGES = [
  { hours: 1, intervalSec: 60, label: '1 小时' },
  { hours: 6, intervalSec: 300, label: '6 小时' },
  { hours: 24, intervalSec: 900, label: '24 小时' },
] as const;
type RangeHours = (typeof RANGES)[number]['hours'];

const qpsRange = ref<RangeHours>(1);
const qpsPoints = ref<{ count: number; ts: string }[]>([]);
const currentRange = computed(
  () => RANGES.find((r) => r.hours === qpsRange.value) ?? RANGES[0],
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
    return { key, label, state, text: LIGHT_TEXT[state] };
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
  if (down.length > 0) return `需关注：${down.map((s) => s.label).join('、')}`;
  return healthServices.value.some((s) => s.state === 'unknown')
    ? '部分组件未配置探测目标'
    : '全部核心服务运行正常';
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
      title="运行概览"
      description="集中查看地址资源、核心服务健康度与 DNS 运行态势。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock-3" class="size-3.5" />
          {{ lastUpdated ? `${dayjs(lastUpdated).format('HH:mm:ss')} 更新` : '等待首次加载' }}
        </span>
        <Button :loading="loading" @click="refreshAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          刷新
        </Button>
      </template>
    </PageHeader>

    <!-- 核心指标 -->
    <div class="ipam-stat-grid">
      <StatCard
        label="今日活跃终端"
        :value="fmtInt(data?.onlineNow)"
        unit="台"
        icon="lucide:monitor-smartphone"
        tone="blue"
        :spark="onlineSpark"
        :hint="`新增 ${fmtInt(data?.newTerminals)} · 离线 ${fmtInt(data?.offlineTerminals)}`"
      />
      <StatCard
        label="DNS 5 分钟 QPS"
        :value="fmtDecimal(data?.dns?.qps5m)"
        unit="QPS"
        icon="lucide:activity"
        tone="cyan"
        :spark="qpsSpark"
        :hint="`近 1 小时均值 ${fmtDecimal(qpsAvgRate)} 次/分`"
      />
      <StatCard
        label="核心服务健康"
        :value="`${healthUp} / ${healthTotal || '—'}`"
        icon="lucide:shield-check"
        :tone="healthTone"
        :hint="healthHint"
        :progress="healthRatio"
      />
      <StatCard
        label="今日 DNS 拦截"
        :value="fmtInt(data?.dns?.blockedToday ?? null)"
        unit="次"
        icon="lucide:shield-ban"
        tone="violet"
        hint="封禁名单命中（今日累计）"
      />
    </div>

    <!-- 活跃终端趋势 + 服务健康 -->
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:users" />
            活跃终端趋势
            <em class="ipam-card-sub">近 24 小时 · 逐小时</em>
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">峰值 {{ fmtInt(onlinePeak) }} 台</span>
        </template>

        <Skeleton v-if="!data" active :paragraph="{ rows: 4 }" />
        <template v-else-if="onlineSeries.length > 0">
          <TrendChart
            :data="onlineSeries"
            :height="196"
            :markers="false"
            bars
            tone="blue"
            unit="台"
          />
          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">今日新增</span>
              <span class="ipam-figure__value">{{ fmtInt(data?.newTerminals) }}</span>
              <span class="ipam-figure__unit">台</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">离线终端</span>
              <span class="ipam-figure__value">{{ fmtInt(data?.offlineTerminals) }}</span>
              <span class="ipam-figure__unit">台</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">小时均值</span>
              <span class="ipam-figure__value">{{ fmtInt(onlineAvg) }}</span>
              <span class="ipam-figure__unit">台</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">双栈联动成功率</span>
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
          <div class="ipam-empty__title">暂无活跃终端趋势</div>
          <div class="ipam-empty__desc">租约数据产生后将自动在此展示。</div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:shield-check" />
            核心服务健康
          </span>
        </template>
        <template #extra>
          <span class="ipam-chip" :data-tone="healthTone">
            {{ healthTone === 'green' ? '正常' : healthTone === 'red' ? '异常' : '待接入' }}
          </span>
        </template>

        <div class="ipam-health-summary">
          <MetricRing
            :value="healthRatio"
            :size="112"
            :thickness="9"
            :tone="healthTone"
            :label="`${healthUp}/${healthTotal || 0}`"
            caption="服务正常"
            :empty="healthTotal === 0"
          />
          <div class="ipam-health-summary__text">
            <div class="ipam-health-summary__title">{{ healthHint }}</div>
            <div class="ipam-hint">探测间隔 30 秒 · 未接入=未配置探测目标</div>
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
          <span>PoC 内存模式下未配置探测目标会显示「未接入」，不影响其他功能。</span>
        </div>
      </Card>
    </div>

    <!-- DNS 查询量 + 热门域名 -->
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:activity" />
            DNS 查询量
            <em class="ipam-card-sub">{{ qpsBucketMin }} 分钟分桶</em>
          </span>
        </template>
        <template #extra>
          <div class="ipam-card-tools">
            <span class="ipam-card-legend">
              <span><i data-kind="peak"></i>峰值</span>
              <span><i data-kind="avg"></i>均值</span>
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
            unit="次"
          />
          <div v-else class="ipam-empty">
            <div class="ipam-empty__icon">
              <IconifyIcon icon="lucide:activity" class="size-5" />
            </div>
            <div class="ipam-empty__title">暂无查询数据</div>
            <div class="ipam-empty__desc">DNS 查询日志接入后将在此展示查询趋势。</div>
          </div>
          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">平均速率</span>
              <span class="ipam-figure__value">{{ fmtDecimal(qpsAvgRate) }}</span>
              <span class="ipam-figure__unit">次/分</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">峰值速率</span>
              <span class="ipam-figure__value">{{ fmtDecimal(qpsPeakRate) }}</span>
              <span class="ipam-figure__unit">次/分</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">区间查询总量</span>
              <span class="ipam-figure__value">{{ fmtInt(qpsTotal) }}</span>
              <span class="ipam-figure__unit">次</span>
            </div>
          </div>
        </template>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:flame" />
            热门查询域名
            <em class="ipam-card-sub">近 1 小时</em>
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/logs-center/search">
            日志明细
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
          <div class="ipam-empty__title">暂无域名数据</div>
          <div class="ipam-empty__desc">DNS 查询日志接入后展示热门域名。</div>
        </div>
      </Card>
    </div>

    <!-- 主机资源 + 地址池利用率 -->
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:server" />
            主机资源
            <em class="ipam-card-sub">宿主整机 · 每 10 秒刷新</em>
          </span>
        </template>
        <template #extra>
          <span v-if="metrics" class="ipam-hint">
            {{ dayjs(metrics.ts).format('HH:mm:ss') }} 采样 · 开机 {{ fmtUptime(metrics.uptimeSec) }}
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
                <small>{{ metrics.cpu.cores }} 核</small>
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
                <span>内存</span>
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
                <span>根分区</span>
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
            <div class="ipam-empty__title">暂无磁盘数据</div>
            <div class="ipam-empty__desc">请确认控制面已挂载宿主 /proc 与 /sys。</div>
          </div>

          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">1 分钟负载</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load1, 2) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">5 分钟负载</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load5, 2) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">15 分钟负载</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load15, 2) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">连续运行</span>
              <span class="ipam-figure__value">{{ fmtUptime(metrics.uptimeSec) }}</span>
            </div>
          </div>
        </template>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:network" />
            地址池利用率 Top 5
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/dhcp/subnets">
            子网管理
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
              <div class="ipam-rank__cidr ipam-mono">{{ p.cidr }}</div>
              <div
                class="ipam-progress"
                :data-tone="toneOfPct(Number(p.pct) || 0)"
              >
                <i :style="{ width: `${Math.min(100, Number(p.pct) || 0)}%` }"></i>
              </div>
              <div class="ipam-rank__meta">
                已用 {{ fmtInt(p.used) }} / 容量 {{ fmtInt(p.capacity) }}
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:network" class="size-5" />
          </div>
          <div class="ipam-empty__title">暂无地址池数据</div>
          <div class="ipam-empty__desc">创建子网并产生租约后展示利用率排名。</div>
        </div>
      </Card>
    </div>
  </div>
</template>
