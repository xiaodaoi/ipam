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

import { useLogWindowStats } from '../use-log-window-stats';

const RANGES = [
  { hours: 1, key: '1h', label: '近 1 小时' },
  { hours: 6, key: '6h', label: '近 6 小时' },
  { hours: 24, key: '24h', label: '近 24 小时' },
  { hours: 168, key: '7d', label: '近 7 天' },
] as const;
type RangeKey = (typeof RANGES)[number]['key'];

const range = ref<RangeKey>('6h');
const window = ref({ from: '', to: '', label: '近 6 小时' });

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
  const r = RANGES.find((x) => x.key === range.value) ?? RANGES[1]!;
  window.value = {
    from: new Date(Date.now() - r.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
    label: r.label,
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

const auditColumns = [
  { dataIndex: 'ts', key: 'ts', title: '时间', width: 96 },
  { dataIndex: 'actor', key: 'actor', title: '调用者', width: 150 },
  { dataIndex: 'actorType', key: 'actorType', title: '类型', width: 76 },
  { dataIndex: 'action', key: 'action', title: '动作', width: 92 },
  { dataIndex: 'resource', key: 'resource', title: '资源', ellipsis: true },
  { dataIndex: 'status', key: 'status', title: '状态', width: 76 },
];
const ACTOR_META: Record<string, { color: string; label: string }> = {
  human: { color: 'blue', label: '人工' },
  bot: { color: 'purple', label: 'Bot' },
  system: { color: 'default', label: '系统' },
};
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
      title="日志概览"
      description="汇总 DHCP 与 DNS 事件规模、查询趋势、热门域名与客户端，并同步展示最近的管理操作审计。"
    >
      <template #actions>
        <div class="ipam-seg">
          <button
            v-for="r in RANGES"
            :key="r.key"
            type="button"
            :data-active="range === r.key"
            @click="setRange(r.key)"
          >
            {{ r.label }}
          </button>
        </div>
        <span class="ipam-page-header__meta">
          更新于 {{ lastUpdated || '—' }}
        </span>
        <Button size="small" :loading="loading" @click="loadAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="事件总量"
        :value="stats ? fmtInt(stats.total) : kpi.failed.value ? '—' : '…'"
        icon="lucide:activity"
        tone="blue"
        :spark="stats?.spark"
        :hint="`${window.label} · 自动刷新 60 秒`"
      />
      <StatCard
        label="DNS 查询"
        :value="stats ? fmtInt(stats.dns) : kpi.failed.value ? '—' : '…'"
        icon="lucide:globe"
        tone="cyan"
        :hint="`占事件总量 ${pct(stats?.dns ?? 0, stats?.total ?? 0)}`"
      />
      <StatCard
        label="DHCP 事件"
        :value="stats ? fmtInt(stats.dhcp) : kpi.failed.value ? '—' : '…'"
        icon="lucide:network"
        tone="green"
        :hint="`占事件总量 ${pct(stats?.dhcp ?? 0, stats?.total ?? 0)}`"
      />
      <StatCard
        label="封禁拦截"
        :value="stats ? fmtInt(stats.blocked) : kpi.failed.value ? '—' : '…'"
        icon="lucide:shield-ban"
        :tone="(stats?.blocked ?? 0) > 0 ? 'red' : 'violet'"
        :hint="(stats?.blocked ?? 0) > 0 ? `占 DNS 查询 ${pct(stats?.blocked ?? 0, stats?.dns ?? 0)}` : '当前时间窗无拦截事件'"
      />
    </div>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card class="xl:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:chart-area" />
            查询趋势
            <em class="ipam-card-sub">{{ bucketSeconds }} 秒分桶</em>
          </span>
        </template>
        <template #extra>
          <span class="ipam-card-legend">
            <span><i data-kind="peak"></i>峰值</span>
            <span><i data-kind="avg"></i>均值</span>
          </span>
        </template>

        <TrendChart
          v-if="qpsPoints.length > 0"
          :data="qpsPoints"
          :height="210"
          :time-format="range === '1h' || range === '6h' ? 'HH:mm' : 'MM-DD HH:mm'"
          tone="cyan"
          unit="次"
        />
        <div v-else class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:chart-area" class="size-5" />
          </div>
          <div class="ipam-empty__title">暂无趋势数据</div>
          <div class="ipam-empty__desc">
            ClickHouse 中还没有可聚合的事件，触发一次 DNS 查询或 DHCP 续约后即可看到曲线。
          </div>
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
            <span class="ipam-figure__label">拦截占比</span>
            <span class="ipam-figure__value">{{ pct(stats?.blocked ?? 0, stats?.total ?? 0) }}</span>
            <span class="ipam-figure__unit">全量事件</span>
          </div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:pie-chart" />
            流量构成
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ window.label }}</span>
        </template>

        <div v-if="stats && stats.total > 0" class="ipam-compose">
          <MetricRing
            :value="dnsShare"
            :size="112"
            :thickness="10"
            tone="cyan"
            :label="`${dnsShare}%`"
            caption="DNS 查询占比"
          />
          <div class="ipam-dist">
            <div class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>DNS 查询</span>
                <span>{{ fmtInt(stats.dns) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="cyan" :style="{ width: `${Math.max(2, (stats.dns / stats.total) * 100)}%` }"></i>
              </div>
            </div>
            <div class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>DHCP 事件</span>
                <span>{{ fmtInt(stats.dhcp) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="green" :style="{ width: `${Math.max(2, (stats.dhcp / stats.total) * 100)}%` }"></i>
              </div>
            </div>
            <div class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>封禁拦截（DNS 子集）</span>
                <span>{{ fmtInt(stats.blocked) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="red" :style="{ width: `${Math.max(2, (stats.blocked / Math.max(1, stats.dns)) * 100)}%` }"></i>
              </div>
            </div>
            <div v-if="otherCount > 0" class="ipam-dist__row">
              <div class="ipam-dist__head">
                <span>未分类事件</span>
                <span>{{ fmtInt(otherCount) }}</span>
              </div>
              <div class="ipam-dist__bar">
                <i data-tone="amber" :style="{ width: `${Math.max(2, (otherCount / stats.total) * 100)}%` }"></i>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">暂无流量数据</div>
          <div class="ipam-empty__desc">时间窗内没有 DHCP/DNS 事件，接入日志采集后自动汇总。</div>
        </div>

        <div class="ipam-note mt-3">
          <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
          <span>拦截事件同时计入 DNS 查询；比例按当前时间窗实时计算。</span>
        </div>
      </Card>
    </div>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:flame" />
            热门域名
            <em class="ipam-card-sub">Top 8</em>
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/logs-center/search">
            日志明细
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
          <div class="ipam-empty__title">暂无域名数据</div>
          <div class="ipam-empty__desc">DNS 查询日志接入后展示热门域名排行。</div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:monitor-smartphone" />
            活跃客户端
            <em class="ipam-card-sub">Top 8 · 按 MAC</em>
          </span>
        </template>
        <template #extra>
          <RouterLink class="ipam-link" to="/logs-center/search">
            日志明细
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
          <div class="ipam-empty__title">暂无客户端数据</div>
          <div class="ipam-empty__desc">DHCP 事件携带 MAC 后即可按客户端聚合。</div>
        </div>
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />
          最近操作审计
          <em class="ipam-card-sub">{{ window.label }}</em>
        </span>
      </template>
      <template #extra>
        <RouterLink class="ipam-link" to="/logs-center/audit">
          查看全部
          <IconifyIcon icon="lucide:arrow-up-right" class="size-3" />
        </RouterLink>
      </template>

      <Table
        :columns="auditColumns"
        :data-source="recentAudits"
        :locale="{ emptyText: '当前时间窗没有管理操作记录' }"
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
