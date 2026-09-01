<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import type { Dayjs } from 'dayjs';

import { Button, Card, DatePicker, Input, Select, Table, TabPane, Tabs, Tag, message } from 'ant-design-vue';

import { exportLogsCsv, getLogQps, listLogTop, listLogs, type LogQuery } from '#/api/ipam';

const RANGE_PRESETS = [
  { label: '近1小时', hours: 1 },
  { label: '近6小时', hours: 6 },
  { label: '近24小时', hours: 24 },
];
const hours = ref(6);
const customRange = ref<[Dayjs, Dayjs] | undefined>(undefined);

function isoAgo(h: number): string {
  return new Date(Date.now() - h * 3600_000).toISOString();
}
function nowIso(): string {
  return new Date().toISOString();
}

/** 时间窗：自定义区间优先，否则用预设小时数 */
function timeWindow(): { from: string; to: string } {
  if (customRange.value) {
    return {
      from: customRange.value[0].toISOString(),
      to: customRange.value[1].toISOString(),
    };
  }
  return { from: isoAgo(hours.value), to: nowIso() };
}

// ── 检索 ──
const filterType = ref<string>();
const filterDomain = ref<string>();
const filterAnswerIp = ref<string>();
const rows = ref<any[]>([]);
const nextCursor = ref('');
const total = ref(0);
const loading = ref(false);
let cur: LogQuery = { from: isoAgo(6) };

async function loadLogs(cursor?: string) {
  loading.value = true;
  try {
    cur = { ...timeWindow() };
    if (filterType.value) cur.type = filterType.value;
    if (filterDomain.value) cur.domain = filterDomain.value;
    if (filterAnswerIp.value) cur.answerIp = filterAnswerIp.value;
    cur.pageSize = 50;
    if (cursor) cur.cursor = cursor;
    const page = await listLogs(cur);
    rows.value = cursor ? [...rows.value, ...page.items] : page.items;
    nextCursor.value = page.nextCursor ?? '';
    total.value = page.total ?? 0;
  } finally {
    loading.value = false;
  }
}

async function onExport() {
  const params: Record<string, string> = timeWindow();
  if (filterType.value) params.type = filterType.value;
  if (filterDomain.value) params.domain = filterDomain.value;
  if (filterAnswerIp.value) params.answerIp = filterAnswerIp.value;
  try {
    await exportLogsCsv(params);
    message.success('导出已开始下载');
  } catch (e) {
    message.error((e as Error).message || '导出失败');
  }
}
const columns = [
  { title: '时间', dataIndex: 'ts', width: 190 },
  { title: '类型', dataIndex: 'type', width: 80 },
  { title: '域名', dataIndex: 'domain' },
  { title: '客户端', dataIndex: 'clientMac', width: 130 },
  { title: 'IP', dataIndex: 'clientIp', width: 140 },
  { title: '应答码', dataIndex: 'rcode', width: 100 },
  { title: '应答IP', dataIndex: 'answerIp', width: 140 },
  { title: '应答服务器', dataIndex: 'sip', width: 140 },
  { title: '动作', dataIndex: 'action', width: 120 },
];
function fmtTs(v: string) {
  return new Date(v).toLocaleString();
}
const TAG_COLOR: Record<string, string> = {
  blocked: 'red', resolve: 'green', dns_query: 'blue', lease_commit: 'cyan',
};

// ── TopN / QPS ──
const topItems = ref<any[]>([]);
const qpsPoints = ref<any[]>([]);
const bars = computed(() => {
  const max = Math.max(1, ...qpsPoints.value.map((p) => p.count));
  return qpsPoints.value.map((p) => ({
    ts: p.ts,
    count: p.count,
    pct: Math.round((p.count / max) * 100),
  }));
});

async function loadAgg() {
  const base = timeWindow();
  const [top, qps] = await Promise.all([
    listLogTop({ ...base, by: 'domain', limit: 8 }),
    getLogQps({ ...base, intervalSec: Math.max(60, Math.round((hours.value * 3600) / 60)) }),
  ]);
  topItems.value = top.items ?? [];
  qpsPoints.value = qps.points ?? [];
}

function reload() {
  void loadLogs();
  void loadAgg();
}
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  reload();
  timer = setInterval(loadAgg, 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="p-4">
    <Card class="mb-4">
      <div class="flex flex-wrap items-center gap-3">
        <span>时间窗</span>
        <Select
          v-model:value="hours"
          style="width: 110px"
          :disabled="!!customRange"
          :options="RANGE_PRESETS.map((p) => ({ value: p.hours, label: p.label }))"
        />
        <DatePicker.RangePicker
          v-model:value="customRange"
          :show-time="{ format: 'HH:mm:ss' }"
          format="YYYY-MM-DD HH:mm:ss"
          style="width: 360px"
          :placeholder="['开始日期时间', '结束日期时间']"
          @change="reload()"
        />
        <Select v-model:value="filterType" allow-clear placeholder="类型" style="width: 100px" :options="[{ value: 'dhcp', label: 'DHCP' }, { value: 'dns', label: 'DNS' }]" />
        <Input v-model:value="filterDomain" allow-clear placeholder="域名子串" style="width: 200px" @press-enter="reload()" />
        <Input v-model:value="filterAnswerIp" allow-clear placeholder="应答IP（反查域名）" style="width: 170px" @press-enter="reload()" />
        <Button type="primary" @click="reload()">查询</Button>
        <Button class="ml-1" @click="onExport">导出 CSV</Button>
        <span class="text-xs text-gray-400">满足条件 {{ total }} 条</span>
      </div>
    </Card>

    <Tabs>
      <TabPane key="list" tab="日志明细">
        <Table
          :data-source="rows"
          :columns="columns"
          :loading="loading"
          row-key="(_, i) => String(i)"
          size="small"
          :pagination="{
            position: ['bottomLeft'],
            pageSize: 50,
            pageSizeOptions: ['20', '50', '100', '200'],
            showSizeChanger: true,
            showTotal: (t: number) => `共 ${t} 条`,
          }"
        >
          <template #bodyCell="{ column, record, index }">
            <template v-if="column.dataIndex === 'ts'">{{ fmtTs(record.ts) }}</template>
            <template v-else-if="column.dataIndex === 'action'">
              <Tag :color="TAG_COLOR[record.action] || 'default'">{{ record.action }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'loadMore'">
              <Button v-if="nextCursor && index === rows.length - 1" size="small" @click="loadLogs(nextCursor)">加载更多</Button>
            </template>
          </template>
        </Table>
      </TabPane>

      <TabPane key="top" tab="TopN 域名">
        <Table :data-source="topItems" :columns="[{ title: '域名', dataIndex: 'key' }, { title: '次数', dataIndex: 'count', width: 120 }]" row-key="key" size="small" :pagination="false" />
      </TabPane>

      <TabPane key="qps" tab="QPS 曲线">
        <div class="flex h-48 items-end gap-[2px] px-2">
          <div
            v-for="(b, i) in bars"
            :key="i"
            class="min-w-[4px] flex-1 rounded-t bg-blue-400/80"
            :style="{ height: b.pct + '%' }"
            :title="`${new Date(b.ts).toLocaleTimeString()} · ${b.count}`"
          ></div>
        </div>
        <div class="mt-1 flex justify-between px-2 text-xs text-gray-400">
          <span>{{ bars[0] ? new Date(bars[0]!.ts).toLocaleTimeString() : '' }}</span>
          <span>now</span>
        </div>
      </TabPane>
    </Tabs>
  </div>
</template>