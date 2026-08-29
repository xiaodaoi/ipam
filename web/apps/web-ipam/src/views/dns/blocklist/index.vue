<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';

import { Button, Card, Table, Tag, message } from 'ant-design-vue';

import { deleteBlocklist, listBlocklists, syncBlocklist, type Blocklist } from '#/api/ipam';

const rows = ref<Blocklist[]>([]);
const loading = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
  loading.value = true;
  try {
    const d = await listBlocklists();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
async function sync(id?: string) {
  if (id) await syncBlocklist(id);
  await load();
}
async function removeList(id: string) {
  await deleteBlocklist(id);
  message.success('名单已删除');
  await load();
}
const KIND_TEXT: Record<string, string> = { builtin: '内置', custom: '自定义', feed: '订阅' };
const KIND_COLOR: Record<string, string> = { builtin: 'default', custom: 'blue', feed: 'purple' };
function fmtSync(v?: string) {
  return v ? new Date(v).toLocaleString() : '—';
}
onMounted(() => {
  void load();
  timer = setInterval(load, 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <Card title="封禁名单库">
    <template #extra>
      <Button size="small" @click="load()">刷新（30s 自动）</Button>
    </template>
    <Table
      :data-source="rows"
      :columns="[
        { title: '名称', dataIndex: 'name' },
        { title: '类型', dataIndex: 'kind', width: 100 },
        { title: '订阅地址', dataIndex: 'syncUrl' },
        { title: '上次同步', dataIndex: 'lastSync', width: 170 },
        { title: '版本', dataIndex: 'version', width: 80 },
        { title: '操作', key: 'op', width: 110 },
      ]"
      row-key="id"
      size="small"
      :loading="loading"
      :pagination="false"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.dataIndex === 'kind'">
          <Tag :color="KIND_COLOR[record.kind]">{{ KIND_TEXT[record.kind] ?? record.kind }}</Tag>
        </template>
        <template v-else-if="column.dataIndex === 'lastSync'">
          {{ fmtSync(record.lastSync) }}
        </template>
        <template v-else-if="column.key === 'op'">
          <Button v-if="record.kind === 'feed'" size="small" @click="sync(record.id)">立即同步</Button>
          <Button v-if="record.kind !== 'builtin'" class="ml-1" size="small" danger @click="removeList(record.id)">删除</Button>
        </template>
      </template>
    </Table>
    <div class="mt-2 text-xs text-gray-400">
      订阅源按计划自动同步（失败保旧版）；策略分组与提示页定制属后续批次。
    </div>
  </Card>
</template>