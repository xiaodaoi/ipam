<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { Button, Card, Checkbox, Input,  Tag, message } from 'ant-design-vue';

import { useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import {
  createRole,
  deleteRole,
  listRoles,
  updateRole,
  type RoleRow,
} from '#/api/ipam';

// 权限域（M2-035：6 域 × read/write = 12 权限点）
const PERM_DOMAINS = [
  { key: 'dash', label: '仪表盘' },
  { key: 'logs', label: '日志' },
  { key: 'dhcp', label: 'DHCP 服务' },
  { key: 'dns', label: 'DNS 服务' },
  { key: 'system', label: '系统管理' },
  { key: 'assets', label: '资产台账' },
];

const rows = ref<RoleRow[]>([]);
const loading = ref(false);
const modal = reactive({
  editing: '',
  name: '',
  perms: [] as string[],
});
const [RoleModal, roleModalApi] = useVbenModal({
  draggable: true,
  title: '新建角色',
  confirmText: '保存',
  onConfirm: () => save(),
});

const gridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '角色名', minWidth: 100 },
    { field: 'builtin', title: '类型', width: 90, slots: { default: 'builtin' } },
    { field: 'permCount', title: '权限点数', width: 100, slots: { default: 'permCount' } },
    { field: 'op', title: '操作', width: 150, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  rowConfig: { keyField: 'name' },
});
const [RoleGrid] = useVbenVxeGrid({ gridOptions });

async function load() {
  loading.value = true;
  try {
    const d = await listRoles();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  modal.editing = '';
  modal.name = '';
  modal.perms = [];
  roleModalApi.setState({ title: '新建角色', confirmText: '创建' });
  roleModalApi.open();
}

function openEdit(r: RoleRow) {
  modal.editing = r.name;
  modal.name = r.name;
  modal.perms = [...(r.permissions ?? [])];
  roleModalApi.setState({ title: `编辑角色：${r.name}`, confirmText: '保存修改' });
  roleModalApi.open();
}

function togglePerm(p: string, checked: boolean) {
  const arr = modal.perms;
  if (checked && !arr.includes(p)) arr.push(p);
  if (!checked) modal.perms = arr.filter((x) => x !== p);
}

async function save() {
  const perms = modal.perms;
  if (modal.editing) {
    await updateRole(modal.editing, { permissions: perms });
    message.success('角色权限已更新');
  } else {
    if (!modal.name) {
      message.warning('请填写角色名');
      return;
    }
    if (perms.length === 0) {
      message.warning('请至少勾选一个权限点');
      return;
    }
    await createRole({ name: modal.name, permissions: perms });
    message.success('角色已创建');
  }
  roleModalApi.close();
  await load();
}

async function remove(r: RoleRow) {
  await deleteRole(r.name);
  message.success(`角色「${r.name}」已删除`);
  await load();
}

onMounted(load);
</script>

<template>
  <div class="p-4">
<Card title="角色管理（RBAC 权限点：域:read | 域:write）">
    <template #extra>
      <Button type="primary" size="small" @click="openCreate">+ 新建角色</Button>
    </template>
    <RoleGrid :table-data="rows">
      <template #builtin="{ row }">
        <Tag :color="row.builtin ? 'blue' : 'green'">{{ row.builtin ? '内置' : '自定义' }}</Tag>
      </template>
      <template #permCount="{ row }">
        {{ (row.permissions ?? []).length }} / 12
      </template>
      <template #op="{ row }">
        <div class="flex items-center gap-1">
          <Button size="small" @click="openEdit(row as RoleRow)">编辑</Button>
          <Button v-if="!row.builtin" size="small" danger @click="remove(row as RoleRow)">删除</Button>
        </div>
      </template>
    </RoleGrid>

    <RoleModal>
      <div v-if="!modal.editing" class="mb-3">
        <div class="mb-1 text-xs text-gray-400">角色名（唯一，创建后不可改）</div>
        <Input v-model:value="modal.name" placeholder="如 network-ops" />
      </div>
      <div class="grid grid-cols-2 gap-2">
        <div
          v-for="dom in PERM_DOMAINS"
          :key="dom.key"
          class="rounded border border-gray-200 p-2"
        >
          <div class="mb-1 text-xs font-medium">{{ dom.label }}</div>
          <Checkbox
            :checked="modal.perms.includes(dom.key + ':read')"
            @change="(e: any) => togglePerm(dom.key + ':read', e.target.checked)"
          >
            查看
          </Checkbox>
          <Checkbox
            :checked="modal.perms.includes(dom.key + ':write')"
            @change="(e: any) => togglePerm(dom.key + ':write', e.target.checked)"
          >
            编辑
          </Checkbox>
        </div>
      </div>
    </RoleModal>
  </Card>
  </div>
</template>
