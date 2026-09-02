<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import { useUserStore } from '@vben/stores';

import {
  Button,
  Card,
  Input,
  Modal,
  Select,
  Switch,
  Tag,
  message,
} from 'ant-design-vue';

import {
  createUser,
  deleteUser,
  listUsers,
  updateUser,
  type UserRow,
} from '#/api/ipam';

const userStore = useUserStore();
const rows = ref<UserRow[]>([]);
const loading = ref(false);
const form = ref({ displayName: '', password: '', roles: 'user', username: '' });
const reset = ref({ id: '', open: false, password: '' });
let timer: ReturnType<typeof setInterval> | undefined;

const myUsername = userStore.userInfo?.username as unknown as string | undefined;

async function load() {
  loading.value = true;
  try {
    const d = await listUsers();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
const [UserModal, userModalApi] = useVbenModal({ draggable: true, title: '创建用户', confirmText: '创建用户', onConfirm: () => add() });
async function add() {
  if (!form.value.username || form.value.password.length < 8) {
    message.warning('用户名必填，口令至少 8 位');
    return;
  }
  try {
    await createUser({
      displayName: form.value.displayName || undefined,
      password: form.value.password,
      roles: [form.value.roles],
      username: form.value.username,
    });
    message.success('用户已创建');
    form.value = { displayName: '', password: '', roles: 'user', username: '' };
    userModalApi.close();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '创建失败');
  }
  await load();
}
async function toggle(id: string, enabled: boolean) {
  try {
    await updateUser(id, { enabled });
    message.success(enabled ? '已启用' : '已停用');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  await load();
}
async function doReset() {
  try {
    await updateUser(reset.value.id, { password: reset.value.password });
    message.success('口令已重置');
    reset.value = { id: '', open: false, password: '' };
  } catch (e) {
    message.error(e instanceof Error ? e.message : '重置失败');
  }
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteUser(id);
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

const gridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'username', title: '登录名', width: 200 },
    { field: 'displayName', title: '显示名', width: 200, slots: { default: 'displayName' } },
    { field: 'roles', title: '角色', width: 200, slots: { default: 'roles' } },
    { field: 'enabled', title: '状态', width: 90, slots: { default: 'enabled' } },
    { field: 'createdAt', title: '创建时间', width: 170, slots: { default: 'createdAt' } },
    { field: 'op', title: '操作', width: 170, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  rowConfig: { keyField: 'id' },
});
const [UsrGrid] = useVbenVxeGrid({ gridOptions });

onBeforeUnmount(() => timer && clearInterval(timer));

const ROLE_COLOR: Record<string, string> = { admin: 'gold', user: 'default' };
const ROLE_TEXT: Record<string, string> = { admin: '管理员', user: '只读' };
const isSelf = (username?: string) => !!myUsername && username === myUsername;
</script>

<template>
  <Card title="用户与角色">
    <template #extra>
      <Button size="small" @click="load()">刷新（15s 自动）</Button>
    </template>

    <div class="mb-4">
      <Button type="primary" size="small" @click="userModalApi.open()">+ 创建用户</Button>
    </div>
    <UserModal class="w-[560px]">
    <div class="flex flex-wrap items-end gap-2">
      <div>
        <div class="mb-1 text-xs text-gray-400">登录名</div>
        <Input v-model:value="form.username" style="width: 140px" placeholder="字母数字 _.-" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">显示名</div>
        <Input v-model:value="form.displayName" style="width: 120px" placeholder="可选" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">初始口令（≥8 位）</div>
        <Input.Password v-model:value="form.password" style="width: 160px" placeholder="至少 8 位" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">角色</div>
        <Select v-model:value="form.roles" style="width: 110px"
          :options="[{ value: 'admin', label: '管理员（可写）' }, { value: 'user', label: '只读' }]" />
      </div>
      <div class="w-full text-xs text-gray-400">
        口令 bcrypt 落库；user 角色写操作将被 RBAC 拦截（403）。
      </div>
    </div>
    </UserModal>

        <UsrGrid :table-data="rows">
      <template #displayName="{ row }">
        {{ row.displayName || '-' }}
      </template>
      <template #roles="{ row }">
        <Tag v-for="r in row.roles" :key="r" :color="ROLE_COLOR[r]">
          {{ ROLE_TEXT[r] ?? r }}
        </Tag>
      </template>
      <template #enabled="{ row }">
        <Switch :checked="row.enabled" :disabled="isSelf(row.username)" size="small" @change="(v: any) => toggle(row.id, !!v)" />
      </template>
      <template #createdAt="{ row }">
        <span class="text-xs text-gray-400">{{ String(row.createdAt).slice(0, 19).replace('T', ' ') }}</span>
      </template>
      <template #op="{ row }">
        <div class="flex items-center gap-1">
          <Button size="small" @click="reset = { id: row.id, open: true, password: '' }">重置口令</Button>
          <Button size="small" danger :disabled="isSelf(row.username)" @click="remove(row.id)">删除</Button>
        </div>
      </template>
    </UsrGrid>

    <Modal
      v-model:open="reset.open"
      title="重置口令"
      ok-text="确认重置"
      cancel-text="取消"
      @ok="doReset"
    >
      <Input.Password
        v-model:value="reset.password"
        placeholder="新口令（至少 8 位）"
      />
    </Modal>
  </Card>
</template>