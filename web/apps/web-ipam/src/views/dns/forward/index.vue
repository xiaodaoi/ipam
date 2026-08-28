<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { Button, Card, Input, Select, Switch, Table } from 'ant-design-vue';

import {
  createForwardRule,
  deleteForwardRule,
  listForwardRules,
  listUpstreams,
  type ForwardRule,
  type Upstream,
} from '#/api/ipam';

const rows = ref<ForwardRule[]>([]);
const upstreams = ref<Upstream[]>([]);
const loading = ref(false);
const form = ref({ domain: '', upstreamId: '', note: '' });

async function load() {
  loading.value = true;
  try {
    const [fr, up] = await Promise.all([listForwardRules(), listUpstreams()]);
    rows.value = fr.items ?? [];
    upstreams.value = up.items ?? [];
  } finally {
    loading.value = false;
  }
}
function upstreamName(id: string): string {
  return upstreams.value.find((u) => u.id === id)?.name ?? id.slice(0, 8);
}
async function add() {
  if (!form.value.domain || !form.value.upstreamId) return;
  await createForwardRule({
    domain: form.value.domain.endsWith('.') ? form.value.domain : `${form.value.domain}.`,
    upstreamIds: [form.value.upstreamId],
    enabled: true,
    note: form.value.note,
  });
  form.value = { domain: '', upstreamId: '', note: '' };
  await load();
}
async function remove(id?: string) {
  if (id) await deleteForwardRule(id);
  await load();
}
onMounted(load);
</script>

<template>
  <Card title="条件转发规则（域名后缀 → 专属上游）">
    <div class="mb-3 flex flex-wrap items-center gap-2">
      <Input v-model:value="form.domain" placeholder="域名后缀 如 corp.local" style="width: 200px" />
      <span class="text-xs">→</span>
      <Select v-model:value="form.upstreamId" placeholder="上游" style="width: 180px"
        :options="upstreams.map((u) => ({ value: u.id, label: u.name }))" />
      <Input v-model:value="form.note" placeholder="备注（可选）" style="width: 160px" />
      <Button type="primary" @click="add">添加</Button>
    </div>
    <Table
      :data-source="rows"
      :columns="[
        { title: '域名后缀', dataIndex: 'domain' },
        { title: '上游', dataIndex: 'upstreamIds' },
        { title: '启用', dataIndex: 'enabled', width: 80 },
        { title: '备注', dataIndex: 'note' },
        { title: '操作', key: 'op', width: 80 },
      ]"
      row-key="id"
      size="small"
      :loading="loading"
      :pagination="false"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.dataIndex === 'upstreamIds'">
          {{ (record.upstreamIds ?? []).map(upstreamName).join(', ') }}
        </template>
        <template v-else-if="column.dataIndex === 'enabled'">
          <Switch :checked="record.enabled" size="small" disabled />
        </template>
        <template v-else-if="column.key === 'op'">
          <Button size="small" danger @click="remove(record.id)">删除</Button>
        </template>
      </template>
    </Table>
    <div class="mt-2 text-xs text-gray-400">最长后缀优先匹配；未命中走默认上游。</div>
  </Card>
</template>