<script setup lang="ts">
import { onBeforeUnmount, reactive, onMounted, ref } from 'vue';
import { useVbenDrawer, useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import { Button, Card, Input, Select,  Tag, message } from 'ant-design-vue';

import {
  addBlocklistEntry,
  compilePolicyGroup,
  createBlocklist,
  createPolicyGroup,
  deleteBlocklist,
  deleteBlocklistEntry,
  listBlocklistEntries,
  listBlocklists,
  listPolicyGroups,
  syncBlocklist,
  type Blocklist,
  type BlocklistEntryRow,
  type PolicyGroupRow,
} from '#/api/ipam';

const rows = ref<Blocklist[]>([]);
const loading = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

const entries = ref<BlocklistEntryRow[]>([]);
const eLoading = ref(false);
const selId = ref('');
const selName = ref('');
const eForm = ref<{ pattern: string; triggerType: 'qname' | 'response_ip'; action: 'nxdomain' | 'drop' | 'tcp_only' | 'redirect' }>({ pattern: '', triggerType: 'qname', action: 'nxdomain' });
const cForm = ref<{ name: string; kind: 'builtin' | 'custom' | 'feed'; syncUrl: string }>({ name: '', kind: 'custom', syncUrl: '' });
const pgRows = ref<PolicyGroupRow[]>([]);
const pgLoading = ref(false);
const pgForm = ref<{ name: string; viewName: string; cidrs: string; listIds: string[] }>({ name: '', viewName: 'recursor', cidrs: '', listIds: [] });
const pgResult = ref<Record<string, unknown> | null>(null);
const pgGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', minWidth: 120 },
    { field: 'viewName', title: 'view', width: 110 },
    { field: 'cidrs', title: 'CIDRs', minWidth: 160 },
    { field: 'lists', title: '名单数', width: 90, slots: { default: 'lists' } },
    { field: 'op', title: '操作', width: 100, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: pgLoading.value,
  rowConfig: { keyField: 'id' },
});
const [PgGrid] = useVbenVxeGrid({ gridOptions: pgGridOptions });

const entryGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'pattern', title: 'pattern', width: 220 },
    { field: 'triggerType', title: '触发', width: 120 },
    { field: 'action', title: '动作', width: 120 },
    { field: 'category', title: '分类', width: 90 },
    { field: 'op', title: '操作', width: 100, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: eLoading.value,
  rowConfig: { keyField: 'pattern' },
});
const [EntryGrid] = useVbenVxeGrid({ gridOptions: entryGridOptions });

const listGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', width: 200 },
    { field: 'kind', title: '类型', width: 100, slots: { default: 'kind' } },
    { field: 'syncUrl', title: '订阅地址', width: 280 },
    { field: 'lastSync', title: '上次同步', width: 170, slots: { default: 'lastSync' } },
    { field: 'version', title: '版本', width: 80 },
    { field: 'op', title: '操作', width: 190, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  rowConfig: { keyField: 'id' },
});
const [ListGrid] = useVbenVxeGrid({ gridOptions: listGridOptions });

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
const [CreateModal, createModalApi] = useVbenModal({ draggable: true, title: '新建名单', confirmText: '创建名单', onConfirm: () => createList() });
async function createList() {
  if (!cForm.value.name) return;
  await createBlocklist({ name: cForm.value.name, kind: cForm.value.kind, syncUrl: cForm.value.syncUrl || undefined });
  message.success('名单已创建');
  cForm.value = { name: '', kind: 'custom', syncUrl: '' };
  await load();
  createModalApi.close();
}
async function loadPolicyGroups() {
  pgLoading.value = true;
  try {
    const d = await listPolicyGroups();
    pgRows.value = d.items ?? [];
  } finally {
    pgLoading.value = false;
  }
}
async function createPg() {
  if (!pgForm.value.name) return;
  await createPolicyGroup({
    name: pgForm.value.name,
    viewName: pgForm.value.viewName || 'recursor',
    cidrs: pgForm.value.cidrs.split(',').map((s) => s.trim()).filter(Boolean),
    listIds: pgForm.value.listIds,
  });
  message.success('策略分组已创建');
  pgForm.value = { name: '', viewName: 'recursor', cidrs: '', listIds: [] };
  await loadPolicyGroups();
}
async function compilePg(id: string) {
  pgResult.value = await compilePolicyGroup(id);
  message.success('RPZ 编译完成');
}

// ── 条目 VbenDrawer（M2-046：条目在抽屉中展示 + 客户端分页）──
const [EntriesDrawer, entriesDrawerApi] = useVbenDrawer({
  class: 'w-[780px]',
  footer: false,
  title: '名单条目',
});
async function openEntries(r: Blocklist) {
  selId.value = r.id;
  selName.value = r.name;
  entriesDrawerApi.setState({ title: `名单条目 · ${r.name}` });
  entriesDrawerApi.open();
  await loadEntries();
}
async function loadEntries() {
  if (!selId.value) return;
  eLoading.value = true;
  try {
    const d = await listBlocklistEntries(selId.value);
    entries.value = d.items ?? [];
  } finally {
    eLoading.value = false;
  }
}
async function addEntry() {
  if (!eForm.value.pattern) return;
  await addBlocklistEntry(selId.value, { pattern: eForm.value.pattern, triggerType: eForm.value.triggerType, action: eForm.value.action });
  message.success('条目已添加');
  eForm.value = { pattern: '', triggerType: 'qname', action: 'nxdomain' };
  await loadEntries();
}
async function removeEntry(listId: string, pattern: string) {
  try {
    await deleteBlocklistEntry(listId, pattern);
    message.success('条目已删除');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '删除失败');
  }
  await loadEntries();
}
const KIND_TEXT: Record<string, string> = { builtin: '内置', custom: '自定义', feed: '订阅' };
const KIND_COLOR: Record<string, string> = { builtin: 'default', custom: 'blue', feed: 'purple' };
function fmtSync(v?: string) {
  return v ? new Date(v).toLocaleString() : '—';
}
onMounted(() => {
  void loadPolicyGroups();
  void load();
  timer = setInterval(load, 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="p-4">
<Card title="封禁名单库">
    <template #extra>
      <Button size="small" @click="load()">刷新（30s 自动）</Button>
    </template>
      <div class="mb-2">
        <Button type="primary" size="small" @click="createModalApi.open()">+ 新建名单</Button>
      </div>
      <CreateModal>
      <div class="flex flex-wrap items-end gap-2">
        <Input v-model:value="cForm.name" placeholder="名单名称" style="width: 200px" />
        <Select v-model:value="cForm.kind" style="width: 120px" :options="[{ value: 'custom', label: '自定义' }, { value: 'feed', label: '订阅源' }]" />
        <Input v-if="cForm.kind === 'feed'" v-model:value="cForm.syncUrl" placeholder="订阅 URL" style="width: 260px" />
      </div>
      </CreateModal>
    <ListGrid :table-data="rows">
      <template #kind="{ row }">
        <Tag :color="KIND_COLOR[row.kind]">{{ KIND_TEXT[row.kind] ?? row.kind }}</Tag>
      </template>
      <template #lastSync="{ row }">
        {{ fmtSync(row.lastSync) }}
      </template>
      <template #op="{ row }">
        <div class="flex items-center gap-1">
          <Button size="small" @click="openEntries(row as Blocklist)">条目</Button>
          <Button v-if="row.kind === 'feed'" size="small" @click="sync(row.id)">立即同步</Button>
          <Button v-if="row.kind !== 'builtin'" size="small" danger @click="removeList(row.id)">删除</Button>
        </div>
      </template>
    </ListGrid>
    <Card title="策略分组（view 级 RPZ 应用）" class="mt-3">
      <div class="mb-2 flex flex-wrap items-end gap-2">
        <Input v-model:value="pgForm.name" placeholder="分组名称" style="width: 160px" />
        <Input v-model:value="pgForm.viewName" placeholder="view 名" style="width: 180px" />
        <Input v-model:value="pgForm.cidrs" placeholder="CIDRs 逗号分隔（如 10.0.0.0/8）" style="width: 240px" />
        <Select v-model:value="pgForm.listIds" mode="multiple" placeholder="关联名单" style="min-width: 200px" :options="rows.map((r) => ({ value: r.id, label: r.name }))" />
        <Button type="primary" size="small" @click="createPg">创建分组</Button>
      </div>
      <PgGrid :table-data="pgRows">
        <template #lists="{ row }">{{ (row.listIds ?? []).length }}</template>
        <template #op="{ row }">
          <div class="flex items-center gap-1">
            <Button size="small" @click="compilePg(row.id)">编译</Button>
          </div>
        </template>
      </PgGrid>
      <pre v-if="pgResult" class="mt-2 rounded bg-gray-50 p-2 text-xs">{{ pgResult }}</pre>
    </Card>
    <div class="mt-2 text-xs text-gray-400">
      订阅源按计划自动同步（失败保旧版）；提示页定制属后续批次。
    </div>
  </Card>

  <EntriesDrawer>
    <div class="mb-2 flex flex-wrap items-end gap-2">
      <Input v-model:value="eForm.pattern" placeholder="如 *.gamble.com" style="width: 220px" @pressEnter="addEntry" />
      <Select v-model:value="eForm.triggerType" style="width: 150px" :options="[{ value: 'qname', label: 'qname（域名）' }, { value: 'response_ip', label: 'response_ip（应答IP）' }]" />
      <Select v-model:value="eForm.action" style="width: 130px" :options="[{ value: 'nxdomain', label: 'nxdomain' }, { value: 'drop', label: 'drop' }, { value: 'tcp_only', label: 'tcp_only' }, { value: 'redirect', label: 'redirect' }]" />
      <Button type="primary" size="small" @click="addEntry">添加条目</Button>
    </div>
    <EntryGrid :table-data="entries">
      <template #op="{ row }">
        <div class="flex items-center gap-1">
          <Button size="small" danger @click="removeEntry(row.listId, row.pattern)">删除</Button>
        </div>
      </template>
    </EntryGrid>
  </EntriesDrawer>
  </div>
</template>