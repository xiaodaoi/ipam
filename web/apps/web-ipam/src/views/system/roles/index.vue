<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { Button, Card, Checkbox, Input, Table, Tag, message } from 'ant-design-vue';

import { VbenModal } from '@vben/common-ui';

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
const modal = ref({
  show: false,
  editing: '',
  name: '',
  perms: [] as string[],
});

const cols = [
  { title: '角色名', dataIndex: 'name' },
  { title: '类型', key: 'builtin', width: 90 },
  { title: '权限点数', key: 'permCount', width: 100 },
  { title: '操作', key: 'op', width: 150 },
];

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
  modal.value = { show: true, editing: '', name: '', perms: [] };
}

function openEdit(r: RoleRow) {
  modal.value = {
    show: true,
    editing: r.name,
    name: r.name,
    perms: [...(r.permissions ?? [])],
  };
}

function togglePerm(p: string, checked: boolean) {
  const arr = modal.value.perms;
  if (checked && !arr.includes(p)) arr.push(p);
  if (!checked) modal.value.perms = arr.filter((x) => x !== p);
}

async function save() {
  const perms = modal.value.perms;
  if (modal.value.editing) {
    await updateRole(modal.value.editing, { permissions: perms });
    message.success('角色权限已更新');
  } else {
    if (!modal.value.name) {
      message.warning('请填写角色名');
      return;
    }
    if (perms.length === 0) {
      message.warning('请至少勾选一个权限点');
      return;
    }
    await createRole({ name: modal.value.name, permissions: perms });
    message.success('角色已创建');
  }
  modal.value.show = false;
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
  <Card title="角色管理（RBAC 权限点：域:read | 域:write）">
    <template #extra>
      <Button type="primary" size="small" @click="openCreate">+ 新建角色</Button>
    </template>
    <Table
      :data-source="rows"
      :columns="cols"
      row-key="name"
      size="small"
      :loading="loading"
      :pagination="false"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'builtin'">
          <Tag :color="record.builtin ? 'blue' : 'green'">
            {{ record.builtin ? '内置' : '自定义' }}
          </Tag>
        </template>
        <template v-else-if="column.key === 'permCount'">
          {{ (record.permissions ?? []).length }} / 12
        </template>
        <template v-else-if="column.key === 'op'">
          <Button size="small" class="mr-1" @click="openEdit(record as RoleRow)">
            编辑
          </Button>
          <Button v-if="!record.builtin" size="small" danger @click="remove(record as RoleRow)">
            删除
          </Button>
        </template>
      </template>
    </Table>

    <VbenModal
      v-model:open="modal.show"
      :title="modal.editing ? `编辑角色：${modal.editing}` : '新建角色'"
      draggable
    >
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
      <div class="mt-3 text-right">
        <Button @click="modal.show = false">取消</Button>
        <Button type="primary" class="ml-1" @click="save">保存</Button>
      </div>
    </VbenModal>
  </Card>
</template>
