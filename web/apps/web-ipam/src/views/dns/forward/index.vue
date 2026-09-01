<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import { Button, Card, Input, Select, Switch, Table, message } from 'ant-design-vue';

import {
  createForwardRule,
  deleteForwardRule,
  listForwardRules,
  listUpstreams,
  updateForwardRule,
  type ForwardRule,
  type Upstream,
} from '#/api/ipam';

const rows = ref<ForwardRule[]>([]);
const editingId = ref<string>();
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
  const domain = form.value.domain.endsWith('.') ? form.value.domain : `${form.value.domain}.`;
  if (editingId.value) {
    await updateForwardRule(editingId.value, {
      upstreamIds: [form.value.upstreamId],
      enabled: true,
      note: form.value.note,
    });
  } else {
    await createForwardRule({
      domain,
      upstreamIds: [form.value.upstreamId],
      enabled: true,
      note: form.value.note,
    });
  }
  editingId.value = undefined;
  form.value = { domain: '', upstreamId: '', note: '' };
  formModalApi.close();
  await load();
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '转发规则', confirmText: '添加', onConfirm: () => add() });
function edit(r: ForwardRule) {
  editingId.value = r.id;
  formModalApi.setState({ title: '编辑转发规则', confirmText: '保存修改' });
  formModalApi.open();
  form.value = {
    domain: r.domain, upstreamId: (r.upstreamIds ?? [])[0] ?? '', note: r.note ?? '',
  };
}
async function toggleEnabled(r: ForwardRule, checked: boolean) {
  try {
    await updateForwardRule(r.id, { enabled: checked });
    message.success(checked ? '已启用' : '已停用');
    await load();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteForwardRule(id);
    message.success('转发规则已删除');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '删除失败');
  }
  await load();
}
onMounted(load);
</script>

<template>
  <Card title="条件转发规则（域名后缀 → 专属上游）">
    <div class="mb-3">
      <Button type="primary" size="small" @click="formModalApi.setState({ title: '添加转发规则' }); formModalApi.open()">+ 添加转发规则</Button>
    </div>
    <FormModal class="w-[720px]">
      <div class="space-y-2">
      <div class="flex items-center gap-2">
        <span class="w-16 shrink-0 text-right text-xs text-gray-400">域名后缀</span>
        <Input v-model:value="form.domain" placeholder="如 corp.local（最长后缀优先匹配）" style="width: 260px" />
      </div>
      <div class="flex items-center gap-2">
        <span class="w-16 shrink-0 text-right text-xs text-gray-400">转发上游</span>
        <Select v-model:value="form.upstreamId" placeholder="选择上游 DNS" style="width: 260px"
          :options="upstreams.map((u) => ({ value: u.id, label: u.name }))" />
      </div>
      <div class="flex items-center gap-2">
        <span class="w-16 shrink-0 text-right text-xs text-gray-400">备注</span>
        <Input v-model:value="form.note" placeholder="备注（可选）" style="width: 260px" />
      </div>
      </div>
    </FormModal>
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
          <Switch :checked="record.enabled" size="small" @change="(checked) => toggleEnabled(record as ForwardRule, Boolean(checked))" />
        </template>
        <template v-else-if="column.key === 'op'">
          <Button size="small" class="mr-1" @click="edit(record as ForwardRule)">编辑</Button>
          <Button size="small" danger @click="remove(record.id)">删除</Button>
        </template>
      </template>
    </Table>
    <div class="mt-2 text-xs text-gray-400">最长后缀优先匹配；未命中走默认上游。</div>
  </Card>
</template>