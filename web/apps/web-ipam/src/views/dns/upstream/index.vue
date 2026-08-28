<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';

import { Button, Card, Input, Select, Table, Tag, message } from 'ant-design-vue';

import {
  createUpstream,
  deleteUpstream,
  listUpstreams,
  type Upstream,
} from '#/api/ipam';

const rows = ref<Upstream[]>([]);
const loading = ref(false);
type Proto = 'udp' | 'tcp' | 'dot';
const form = ref<{ name: string; addr: string; protocol: Proto }>({
  name: '', addr: '', protocol: 'udp',
});
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
  loading.value = true;
  try {
    const d = await listUpstreams();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
async function add() {
  if (!form.value.name || !form.value.addr) return;
  try {
    await createUpstream({ ...form.value, addrs: [form.value.addr], weight: 1, enabled: true });
    message.success('上游已添加');
    form.value = { name: '', addr: '', protocol: 'udp' };
  } catch (e) {
    message.error(e instanceof Error ? e.message : '添加失败');
  }
  await load();
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteUpstream(id);
    message.success('已删除');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '删除失败');
  }
  await load();
}
onMounted(() => {
  void load();
  timer = setInterval(load, 15_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));

const PROTO_COLOR: Record<string, string> = { udp: 'blue', tcp: 'cyan', 'dot': 'purple' };
const HEALTH_TEXT: Record<string, string> = { up: '在线', down: '摘除', unknown: '探测中' };
const HEALTH_COLOR: Record<string, string> = { up: 'green', down: 'red', unknown: 'default' };
</script>

<template>
  <Card title="上游 DNS 服务器">
    <template #extra>
      <Button size="small" @click="load()">刷新（15s 自动）</Button>
    </template>
    <div class="mb-3 flex flex-wrap items-center gap-2">
      <Input v-model:value="form.name" placeholder="名称" style="width: 150px" />
      <Input v-model:value="form.addr" placeholder="地址 如 223.5.5.5:53" style="width: 200px" />
      <Select v-model:value="form.protocol" style="width: 90px" :options="[
        { value: 'udp', label: 'UDP' }, { value: 'tcp', label: 'TCP' }, { value: 'dot', label: 'DoT' }]" />
      <Button type="primary" @click="add">添加</Button>
    </div>
    <Table
      :data-source="rows"
      :columns="[
        { title: '名称', dataIndex: 'name' },
        { title: '地址', dataIndex: 'addrs' },
        { title: '协议', dataIndex: 'protocol', width: 90 },
        { title: '探活', dataIndex: 'health', width: 100 },
        { title: '操作', key: 'op', width: 80 },
      ]"
      row-key="id"
      size="small"
      :loading="loading"
      :pagination="false"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.dataIndex === 'addrs'">{{ (record.addrs ?? []).join(', ') }}</template>
        <template v-else-if="column.dataIndex === 'protocol'">
          <Tag :color="PROTO_COLOR[record.protocol]">{{ record.protocol }}</Tag>
        </template>
        <template v-else-if="column.dataIndex === 'health'">
          <Tag :color="HEALTH_COLOR[record.health?.up ? 'up' : record.health?.down === null ? 'unknown' : 'down']">
            {{ HEALTH_TEXT[record.health?.up ? 'up' : 'down'] }}
          </Tag>
          <span v-if="record.health?.rttMs !== undefined && record.health?.rttMs !== null" class="text-xs text-gray-400">{{ record.health.rttMs }}ms</span>
        </template>
        <template v-else-if="column.key === 'op'">
          <Button size="small" danger @click="remove(record.id)">删除</Button>
        </template>
      </template>
    </Table>
  </Card>
</template>