<script setup lang="ts">
import type { SubnetUtilizationPoint } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  message,
  Progress,
  RadioGroup,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  getSubnetUtilization,
  listSubnetUtilization,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';

defineOptions({ name: 'DhcpUtilization' });

const loading = ref(false);
const rows = ref<SubnetUtilizationPoint[]>([]);
const selected = ref<SubnetUtilizationPoint>();
const hours = ref(24);
const series = ref<SubnetUtilizationPoint[]>([]);
const seriesLoading = ref(false);

const avgPct = computed(() => {
  if (rows.value.length === 0) return 0;
  return rows.value.reduce((s, r) => s + r.pct, 0) / rows.value.length;
});
const maxRow = computed(() => {
  let best: SubnetUtilizationPoint | undefined;
  for (const r of rows.value) {
    if (!best || r.pct > best.pct) best = r;
  }
  return best;
});
const highCount = computed(() => rows.value.filter((r) => r.pct >= 85).length);

const trendData = computed(() =>
  series.value.map((p) => ({ ts: p.ts, value: Number(p.pct.toFixed(2)) })),
);

function tone(pct: number) {
  if (pct >= 90) return 'red';
  if (pct >= 85) return 'amber';
  if (pct >= 60) return 'blue';
  return 'green';
}

function statusText(pct: number) {
  if (pct >= 90) return '紧急';
  if (pct >= 85) return '预警';
  if (pct >= 60) return '关注';
  return '正常';
}

function fmtTime(ts?: string) {
  if (!ts) return '—';
  return new Date(ts).toLocaleString();
}

async function load() {
  loading.value = true;
  try {
    const res = await listSubnetUtilization();
    rows.value = res.items ?? [];
    if (selected.value) {
      const keep = rows.value.find((r) => r.subnetId === selected.value?.subnetId);
      if (keep) selected.value = keep;
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '利用率加载失败');
  } finally {
    loading.value = false;
  }
}

async function openTrend(row: Partial<SubnetUtilizationPoint>) {
  selected.value = row as SubnetUtilizationPoint;
  await loadSeries();
}

async function loadSeries() {
  if (!selected.value) return;
  seriesLoading.value = true;
  try {
    const res = await getSubnetUtilization(selected.value.subnetId, hours.value);
    series.value = res.points ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '趋势加载失败');
  } finally {
    seriesLoading.value = false;
  }
}

const columns = [
  { title: '子网', dataIndex: 'subnetName', key: 'name', ellipsis: true },
  { title: 'CIDR', dataIndex: 'cidr', key: 'cidr', width: 180 },
  { title: '已用 / 容量', key: 'usage', width: 140 },
  { title: '利用率', key: 'pct', width: 220 },
  { title: '状态', key: 'state', width: 100 },
  { title: '采样时间', dataIndex: 'ts', key: 'ts', width: 180 },
  { title: '操作', key: 'actions', width: 100 },
];

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Capacity"
      icon="lucide:gauge"
      title="利用率分析"
      description="每 5 分钟采样各 IPv4 子网池的已用地址（绑定/预留），保留 90 天；超 85% 触发告警指标。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock" class="size-3.5" />
          {{ rows.length }} 个子网
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard label="采样子网" :value="rows.length" unit="个" icon="lucide:network" tone="cyan" hint="仅 IPv4 池" />
      <StatCard label="平均利用率" :value="`${avgPct.toFixed(1)}%`" icon="lucide:activity" tone="green" hint="所有子网算术平均" />
      <StatCard
        label="最高利用率"
        :value="maxRow ? `${maxRow.pct.toFixed(1)}%` : '—'"
        icon="lucide:trending-up"
        :tone="maxRow && maxRow.pct >= 85 ? 'red' : 'amber'"
        :hint="maxRow?.subnetName ?? '暂无采样'"
      />
      <StatCard label="超 85% 子网" :value="highCount" unit="个" icon="lucide:alert-triangle" :tone="highCount > 0 ? 'red' : 'violet'" hint="建议扩容或清理" />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title"><IconifyIcon icon="lucide:table" />最新快照</span>
      </template>
      <Table
        :columns="columns"
        :data-source="rows"
        :loading="loading"
        :pagination="{ pageSize: 20, showSizeChanger: false }"
        row-key="subnetId"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'usage'">
            <span class="ipam-mono">{{ record.used }} / {{ record.capacity }}</span>
          </template>
          <template v-else-if="column.key === 'pct'">
            <Progress
              :percent="Number(record.pct.toFixed(1))"
              :stroke-color="record.pct >= 90 ? '#cf1322' : record.pct >= 85 ? '#d46b08' : '#1677ff'"
              size="small"
            />
          </template>
          <template v-else-if="column.key === 'state'">
            <Tag :color="tone(record.pct)" class="m-0">{{ statusText(record.pct) }}</Tag>
          </template>
          <template v-else-if="column.key === 'ts'">{{ fmtTime(record.ts) }}</template>
          <template v-else-if="column.key === 'actions'">
            <Button size="small" type="link" @click="openTrend(record)">趋势</Button>
          </template>
        </template>
      </Table>
    </Card>

    <Card v-if="selected">
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:line-chart" />
          {{ selected.subnetName }} · {{ selected.cidr }}
        </span>
      </template>
      <template #extra>
        <div class="flex items-center gap-2">
          <RadioGroup
            v-model:value="hours"
            option-type="button"
            size="small"
            :options="[
              { label: '24h', value: 24 },
              { label: '3d', value: 72 },
              { label: '7d', value: 168 },
              { label: '30d', value: 720 },
            ]"
            @change="loadSeries"
          />
          <Button size="small" :loading="seriesLoading" @click="loadSeries">刷新</Button>
        </div>
      </template>
      <TrendChart
        :data="trendData"
        :height="220"
        tone="blue"
        unit="%"
        time-format="MM-DD HH:mm"
      />
      <div class="ipam-hint mt-2">
        当前 {{ selected.used }} / {{ selected.capacity }}（{{ selected.pct.toFixed(1) }}%）；采样点 {{ series.length }} 个。
        利用率 = 池内绑定/预留地址数 ÷ 池容量，不含池外静态地址。
      </div>
    </Card>
  </div>
</template>
