<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';

import { Card, Input, Select, Table, Tag } from 'ant-design-vue';

import { listAudits, type AuditPage } from '#/api/ipam';

const rows = ref<any[]>([]);
const nextCursor = ref('');
const loading = ref(false);
const filterActor = ref<string>();
const filterAction = ref<string>();
const filterQ = ref<string>();

const columns = [
  { title: '时间', dataIndex: 'ts', width: 180 },
  { title: '调用者', dataIndex: 'actor', width: 130 },
  { title: '类型', dataIndex: 'actorType', width: 90 },
  { title: '动作', dataIndex: 'action', width: 100 },
  { title: '资源', dataIndex: 'resource' },
  { title: '状态', dataIndex: 'status', width: 80 },
];
const TYPE_COLOR: Record<string, string> = { human: 'blue', bot: 'purple', system: 'default' };
const STATUS_COLOR = (s: number) => (s < 400 ? 'green' : 'red');

async function load(cursor?: string) {
  loading.value = true;
  try {
    const page: AuditPage = await listAudits({
      from: new Date(Date.now() - 7 * 86400_000).toISOString(),
      actorType: filterActor.value,
      action: filterAction.value,
      q: filterQ.value,
      cursor,
      pageSize: 50,
    });
    rows.value = cursor ? [...rows.value, ...page.items] : page.items;
    nextCursor.value = page.nextCursor ?? '';
  } finally {
    loading.value = false;
  }
}
function fmtTs(v: string) {
  return new Date(v).toLocaleString();
}
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  void load();
  timer = setInterval(() => void load(), 60_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="p-4">
    <Card>
      <div class="mb-3 flex flex-wrap items-center gap-3">
        <Select v-model:value="filterActor" allow-clear placeholder="调用者类型" style="width: 130px" :options="[{ value: 'human', label: '人工' }, { value: 'bot', label: 'Bot' }, { value: 'system', label: '系统' }]" />
        <Select v-model:value="filterAction" allow-clear placeholder="动作" style="width: 110px" :options="['create', 'update', 'delete'].map((v) => ({ value: v, label: v }))" />
        <Input v-model:value="filterQ" allow-clear placeholder="资源/路径子串" style="width: 220px" @press-enter="load()" />
        <a-button type="primary" @click="load()">查询</a-button>
        <a-button v-if="nextCursor" @click="load(nextCursor)">加载更多</a-button>
      </div>
      <Table
        :data-source="rows"
        :columns="columns"
        :loading="loading"
        row-key="id"
        size="small"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'ts'">{{ fmtTs(record.ts) }}</template>
          <template v-else-if="column.dataIndex === 'actorType'">
            <Tag :color="TYPE_COLOR[record.actorType]">{{ record.actorType }}</Tag>
          </template>
          <template v-else-if="column.dataIndex === 'status'">
            <Tag :color="STATUS_COLOR(record.status)">{{ record.status }}</Tag>
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>