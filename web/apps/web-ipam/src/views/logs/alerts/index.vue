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
});
const page = ref(1);
const pageSize = ref(20);
const rows = ref<AlertRow[]>([]);
const total = ref(0);
const loading = ref(false);
const stats = ref<AlertStats>();
const lastUpdated = ref('');

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

function activeFilterCount(): number {
  return [
    filters.q.trim(),
    filters.source,
    filters.severity,
    filters.status,
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

    <div class="grid gap-4 lg:grid-cols-2">
      <Card>
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
            <IconifyIcon icon="lucide:shield-ban" />
            {{ $t('logs.alerts.trend.domains') }}
          </span>
        </template>
        <div v-if="topDomains.length > 0" class="ipam-top-list">
          <div v-for="d in topDomains" :key="d.key" class="ipam-top-row">
            <span class="ipam-mono truncate">{{ d.key }}</span>
            <span class="ipam-hint">{{ fmtInt(d.count) }}</span>
            <div class="ipam-top-bar">
              <span
                :style="{ width: `${(d.count / topDomainMax) * 100}%` }"
              ></span>
            </div>
          </div>
        </div>
        <div v-else class="ipam-empty">—</div>
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

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:users" />
          {{ $t('logs.alerts.trend.clients') }}
        </span>
      </template>
      <div v-if="topClients.length > 0" class="ipam-top-list">
        <div v-for="c in topClients" :key="c.key" class="ipam-top-row">
          <span class="ipam-mono truncate">{{ c.key }}</span>
          <span class="ipam-hint">{{ fmtInt(c.count) }}</span>
          <div class="ipam-top-bar">
            <span
              :style="{ width: `${(c.count / topClientMax) * 100}%` }"
            ></span>
          </div>
        </div>
      </div>
      <div v-else class="ipam-empty">—</div>
    </Card>

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
