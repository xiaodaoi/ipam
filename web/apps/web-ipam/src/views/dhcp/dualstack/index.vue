<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { Button, Card, Input, Select, Switch, Table, Tag, message } from 'ant-design-vue';

import {
  createDualstackTemplate,
  deleteDualstackTemplate,
  updateDualstackTemplate,
  listDualstackTemplates,
  type DualstackTemplate,
} from '#/api/ipam';

const rows = ref<DualstackTemplate[]>([]);
const loading = ref(false);
const form = ref<{ name: string; ipv4Cidr: string; ipv6Prefix: string; encoding: 'B' | 'A' | 'CUSTOM'; expr: string; dnsSync: boolean; graceHours: number }>({
  name: '',
  ipv4Cidr: '',
  ipv6Prefix: '',
  encoding: 'B',
  expr: '{v4.hextet4}',
  dnsSync: true,
  graceHours: 24,
});
const editingId = ref('');

async function load() {
  loading.value = true;
  try {
    const d = await listDualstackTemplates();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
function edit(r: DualstackTemplate) {
  editingId.value = r.id;
  form.value = {
    name: r.name, ipv4Cidr: r.ipv4Cidr, ipv6Prefix: r.ipv6Prefix, encoding: r.encoding,
    expr: r.expr, dnsSync: r.dnsSync ?? true, graceHours: r.graceHours ?? 24,
  };
}
function cancelEdit() {
  editingId.value = '';
  form.value = {
    name: '', ipv4Cidr: '', ipv6Prefix: '',
    encoding: 'B', expr: '{v4.hextet4}', dnsSync: true, graceHours: 24,
  };
}
async function add() {
  if (!form.value.name || !form.value.ipv4Cidr || !form.value.ipv6Prefix) return;
  if (editingId.value) {
    await updateDualstackTemplate(editingId.value, { ...form.value });
    message.success('模板已更新');
  } else {
    await createDualstackTemplate({ ...form.value });
    message.success('模板已创建');
  }
  cancelEdit();
  await load();
}
async function remove(id?: string) {
  if (id) await deleteDualstackTemplate(id);
  await load();
}
onMounted(load);

const ENC_TEXT: Record<string, string> = { B: 'B 型', A: 'A 型', CUSTOM: '自定义' };
const EXAMPLE = '例：192.168.0.10 → 2407::192:168:0:10';
</script>

<template>
  <Card title="双栈绑定模板（v4 池 ↔ v6 前缀映射）">
    <template #extra>
      <Button size="small" @click="load()">刷新</Button>
    </template>

    <div class="mb-4 flex flex-wrap items-end gap-2 rounded border border-gray-200 p-3">
      <div>
        <div class="mb-1 text-xs text-gray-400">名称</div>
        <Input v-model:value="form.name" style="width: 150px" placeholder="办公-v4池A" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">IPv4 网段</div>
        <Input v-model:value="form.ipv4Cidr" style="width: 180px" placeholder="192.168.0.0/24" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">IPv6 前缀</div>
        <Input v-model:value="form.ipv6Prefix" style="width: 160px" placeholder="2407::/64" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">编码</div>
        <Select v-model:value="form.encoding" style="width: 100px"
          :options="(['B', 'A', 'CUSTOM'] as const).map((v) => ({ value: v, label: ENC_TEXT[v] }))" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">表达式</div>
        <Input v-model:value="form.expr" style="width: 160px" placeholder="{v4.hextet4}" />
      </div>
      <div class="flex items-center gap-1 pb-[2px]">
        <span class="text-xs">DNS 同步</span>
        <Switch v-model:checked="form.dnsSync" size="small" />
      </div>
      <Button type="primary" @click="add">{{ editingId ? '保存修改' : '创建模板' }}</Button>
      <Button v-if="editingId" class="ml-1" @click="cancelEdit">取消编辑</Button>
      <div class="w-full text-xs text-gray-400">{{ EXAMPLE }}——daemon 按租约 IPv4 最长前缀自动选模板</div>
    </div>

    <Table
      :data-source="rows"
      :columns="[
        { title: '名称', dataIndex: 'name' },
        { title: 'IPv4 网段', dataIndex: 'ipv4Cidr' },
        { title: 'IPv6 前缀', dataIndex: 'ipv6Prefix' },
        { title: '编码', dataIndex: 'encoding', width: 100 },
        { title: 'DNS 同步', dataIndex: 'dnsSync', width: 100 },
        { title: 'grace(h)', dataIndex: 'graceHours', width: 90 },
        { title: '操作', key: 'op', width: 80 },
      ]"
      row-key="id"
      size="small"
      :loading="loading"
      :pagination="false"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.dataIndex === 'encoding'">
          <Tag>{{ ENC_TEXT[record.encoding] ?? record.encoding }}</Tag>
        </template>
        <template v-else-if="column.dataIndex === 'dnsSync'">
          <Tag :color="record.dnsSync ? 'green' : 'default'">{{ record.dnsSync ? '开' : '关' }}</Tag>
        </template>
        <template v-else-if="column.key === 'op'">
          <Button size="small" class="mr-1" @click="edit(record as DualstackTemplate)">编辑</Button>
          <Button size="small" danger @click="remove(record.id)">删除</Button>
        </template>
      </template>
    </Table>
  </Card>
</template>