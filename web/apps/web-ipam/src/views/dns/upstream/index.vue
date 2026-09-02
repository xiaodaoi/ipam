<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import { Button, Card, Input, Select, Tag, message } from 'ant-design-vue';

import {
  createUpstream,
  deleteUpstream,
  listUpstreams,
  updateUpstream,
  type Upstream,
} from '#/api/ipam';

const rows = ref<Upstream[]>([]);
const editingId = ref<string>();
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
    if (editingId.value) {
      await updateUpstream(editingId.value, {
        name: form.value.name, addrs: form.value.addr.split(',').map((s) => s.trim()).filter(Boolean), protocol: form.value.protocol,
      });
      message.success('上游已更新并下发 Kea');
    } else {
      await createUpstream({ ...form.value, addrs: form.value.addr.split(',').map((s) => s.trim()).filter(Boolean), weight: 1, enabled: true });
      message.success('上游已添加');
    }
    editingId.value = undefined;
    form.value = { name: '', addr: '', protocol: 'udp' };
    formModalApi.close();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  await load();
}
function edit(r: Upstream) {
  editingId.value = r.id;
  formModalApi.setState({ title: '编辑上游', confirmText: '保存修改' });
  formModalApi.open();
  form.value = { name: r.name, addr: (r.addrs ?? []).join(','), protocol: (r.protocol as Proto) ?? 'udp' };
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '上游 DNS 服务器', confirmText: '添加', onConfirm: () => add() });
function openAdd() {
  // 注意：不能先调 cancelEdit()（其 close() 是异步的，会在 open() 之后把 isOpen 置回 false）
  editingId.value = undefined;
  form.value = { name: '', addr: '', protocol: 'udp' };
  formModalApi.setState({ title: '添加上游 DNS', confirmText: '添加' });
  formModalApi.open();
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

const gridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', width: 180 },
    { field: 'addrs', title: '地址', width: 380, slots: { default: 'addrs' } },
    { field: 'protocol', title: '协议', width: 90, slots: { default: 'protocol' } },
    { field: 'health', title: '探活', width: 240, slots: { default: 'health' } },
    { field: 'op', title: '操作', width: 150, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  height: 'auto',
  rowConfig: { keyField: 'id' },
});
const [UpGrid] = useVbenVxeGrid({ gridOptions });
const HEALTH_TEXT: Record<string, string> = { up: '在线', down: '摘除', unknown: '探测中' };
const HEALTH_COLOR: Record<string, string> = { up: 'green', down: 'red', unknown: 'default' };
</script>

<template>
  <div class="p-4">
<Card title="上游 DNS 服务器">
    <template #extra>
      <Button size="small" @click="load()">刷新（15s 自动）</Button>
    </template>
    <div class="mb-3">
      <Button type="primary" size="small" @click="openAdd()">+ 添加上游</Button>
    </div>
    <FormModal class="w-[720px]">
      <div class="flex flex-wrap items-center gap-2">
      <Input v-model:value="form.name" placeholder="名称" style="width: 150px" />
      <Input v-model:value="form.addr" placeholder="地址 如 223.5.5.5:53" style="width: 200px" />
      <Select v-model:value="form.protocol" style="width: 90px" :options="[
        { value: 'udp', label: 'UDP' }, { value: 'tcp', label: 'TCP' }, { value: 'dot', label: 'DoT' }]" />
      </div>
    </FormModal>
    <UpGrid :table-data="rows">
      <template #addrs="{ row }">{{ (row.addrs ?? []).join(', ') }}</template>
      <template #protocol="{ row }">
        <Tag :color="PROTO_COLOR[row.protocol]">{{ row.protocol }}</Tag>
      </template>
      <template #health="{ row }">
        <Tag :color="HEALTH_COLOR[row.health?.up ? 'up' : row.health?.down === null ? 'unknown' : 'down']">
          {{ HEALTH_TEXT[row.health?.up ? 'up' : 'down'] }}
        </Tag>
        <span v-if="row.health?.rttMs !== undefined && row.health?.rttMs !== null" class="ml-1 text-xs text-gray-400">{{ row.health.rttMs }}ms</span>
      </template>
      <template #op="{ row }">
        <div class="flex items-center gap-1">
          <Button size="small" @click="edit(row as Upstream)">编辑</Button>
          <Button size="small" danger @click="remove(row.id)">删除</Button>
        </div>
      </template>
    </UpGrid>
  </Card>
  </div>
</template>