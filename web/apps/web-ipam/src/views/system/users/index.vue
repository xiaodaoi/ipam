<script setup lang="ts">
import type { UserRow } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';
import { useUserStore } from '@vben/stores';

import {
  Button,
  Card,
  Input,
  message,
  Modal,
  Popconfirm,
  Select,
  Switch,
  Table,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { createUser, deleteUser, listUsers, updateUser } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const ROLE_META: Record<string, { color: string; desc: string; label: string }> = {
  admin: { color: 'gold', desc: '可读写全部模块', label: '管理员' },
  auditor: { color: 'purple', desc: '全模块只读，适合审计岗', label: '审计员' },
  operator: { color: 'blue', desc: 'DHCP/DNS/资产读写', label: '运维' },
  user: { color: 'default', desc: '全模块只读', label: '只读' },
};
const ROLE_OPTIONS = [
  { label: '管理员（可读写）', value: 'admin' },
  { label: '只读（仅查看）', value: 'user' },
];
const STATUS_OPTIONS = [
  { label: '全部状态', value: 'all' },
  { label: '已启用', value: 'enabled' },
  { label: '已停用', value: 'disabled' },
];

const userStore = useUserStore();
const myUsername = (userStore.userInfo?.username ?? '') as string;

const rows = ref<UserRow[]>([]);
const loading = ref(false);
const keyword = ref('');
const roleFilter = ref('all');
const statusFilter = ref('all');
const autoRefresh = ref(true);
let timer: ReturnType<typeof setInterval> | undefined;

function fmtDateTime(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString('zh-CN', { hour12: false });
}
const isSelf = (username?: string) => !!myUsername && username === myUsername;
function roleMeta(r: string) {
  return ROLE_META[r] ?? { color: 'default', desc: '', label: r };
}
function initial(name?: string): string {
  return (name ?? '?').trim().charAt(0).toUpperCase() || '?';
}

async function load() {
  loading.value = true;
  try {
    const d = await listUsers();
    rows.value = d.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '账号列表加载失败');
  } finally {
    loading.value = false;
  }
}

// ── 筛选与统计 ──
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (roleFilter.value !== 'all' && !(r.roles as string[]).includes(roleFilter.value)) {
      return false;
    }
    if (statusFilter.value === 'enabled' && !r.enabled) return false;
    if (statusFilter.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    return (
      r.username.toLowerCase().includes(kw) ||
      (r.displayName ?? '').toLowerCase().includes(kw)
    );
  });
});
const roleChoices = computed(() => {
  const seen = new Set<string>();
  for (const r of rows.value) for (const role of r.roles) seen.add(role);
  return [
    { label: '全部角色', value: 'all' },
    ...[...seen].map((r) => ({ label: roleMeta(r).label, value: r })),
  ];
});
const total = computed(() => rows.value.length);
const enabledCount = computed(() => rows.value.filter((r) => r.enabled).length);
const adminCount = computed(
  () => rows.value.filter((r) => r.roles.includes('admin')).length,
);
const readOnlyCount = computed(() => total.value - adminCount.value);
const enabledPct = computed(() =>
  total.value > 0 ? (enabledCount.value / total.value) * 100 : 0,
);
const activeFilters = computed(() => {
  const parts = [keyword.value.trim()];
  if (roleFilter.value === 'all') parts.push('');
  else parts.push(roleFilter.value);
  if (statusFilter.value === 'all') parts.push('');
  else parts.push(statusFilter.value);
  return parts.filter(Boolean).length;
});

// ── 创建 / 编辑 / 重置口令 ──
const createForm = reactive({ displayName: '', password: '', roles: 'user', username: '' });
const [CreateModal, createModalApi] = useVbenModal({
  class: 'w-[560px]',
  draggable: true,
  onConfirm: () => add(),
  title: '新建账号',
});

function openCreate() {
  createForm.username = '';
  createForm.displayName = '';
  createForm.password = '';
  createForm.roles = 'user';
  createModalApi.open();
}

async function add() {
  if (!/^[\w.-]{2,64}$/.test(createForm.username.trim())) {
    message.warning('登录名需 2–64 位，仅允许字母、数字与 _ . -');
    return;
  }
  if (createForm.password.length < 8) {
    message.warning('初始口令至少 8 位');
    return;
  }
  try {
    await createUser({
      displayName: createForm.displayName.trim() || undefined,
      password: createForm.password,
      roles: [createForm.roles],
      username: createForm.username.trim(),
    });
    message.success(`账号「${createForm.username.trim()}」已创建`);
    createModalApi.close();
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '创建失败');
  }
}

const editForm = reactive({ displayName: '', enabled: true, id: '', roles: 'user', username: '' });
const [EditModal, editModalApi] = useVbenModal({
  class: 'w-[520px]',
  draggable: true,
  onConfirm: () => saveEdit(),
  title: '编辑账号',
});

function openEdit(row: UserRow) {
  editForm.id = row.id;
  editForm.username = row.username;
  editForm.displayName = row.displayName ?? '';
  editForm.roles = row.roles.includes('admin') ? 'admin' : 'user';
  editForm.enabled = row.enabled;
  editModalApi.open();
}

async function saveEdit() {
  try {
    await updateUser(editForm.id, {
      displayName: editForm.displayName.trim(),
      enabled: editForm.enabled,
      roles: [editForm.roles],
    });
    message.success('账号信息已更新');
    editModalApi.close();
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '更新失败');
  }
}

async function toggle(row: UserRow, enabled: boolean) {
  try {
    await updateUser(row.id, { enabled });
    message.success(enabled ? `已启用「${row.username}」` : `已停用「${row.username}」`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}

const reset = reactive({ id: '', open: false, password: '', username: '' });
function openReset(row: UserRow) {
  reset.id = row.id;
  reset.username = row.username;
  reset.password = '';
  reset.open = true;
}
async function doReset() {
  if (reset.password.length < 8) {
    message.warning('新口令至少 8 位');
    return;
  }
  try {
    await updateUser(reset.id, { password: reset.password });
    message.success(`「${reset.username}」的口令已重置`);
    reset.open = false;
  } catch (error) {
    message.error(error instanceof Error ? error.message : '重置失败');
  }
}

async function remove(row: UserRow) {
  try {
    await deleteUser(row.id);
    message.success(`账号「${row.username}」已删除`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}

function toggleAuto(on: boolean) {
  clearInterval(timer);
  timer = undefined;
  if (on) timer = setInterval(() => void load(), 15_000);
}

function reload() {
  void load();
}
function resetFilters() {
  keyword.value = '';
  roleFilter.value = 'all';
  statusFilter.value = 'all';
}

const columns = [
  { dataIndex: 'username', key: 'account', title: '账号', minWidth: 220 },
  { dataIndex: 'roles', key: 'roles', title: '角色', width: 140 },
  { dataIndex: 'enabled', key: 'enabled', title: '状态', width: 130 },
  { dataIndex: 'createdAt', key: 'createdAt', title: '创建时间', width: 170 },
  { dataIndex: 'op', key: 'op', title: '操作', width: 240 },
];

onMounted(() => {
  void load();
  timer = setInterval(() => void load(), 15_000);
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:users"
      title="账号管理"
      description="管理控制面登录账号、角色分配与启停状态；口令使用 bcrypt 存储，停用或改密会立即吊销该账号存量会话。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:user-check" class="size-3.5" />
          {{ enabledCount }}/{{ total }} 个账号已启用
        </span>
        <Button size="small" :loading="loading" @click="reload">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:user-plus" class="mr-1 size-3.5" />新建账号
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="账号总数"
        :value="total"
        icon="lucide:users"
        tone="blue"
        :hint="`当前登录：${myUsername || '—'}`"
      />
      <StatCard
        label="已启用账号"
        :value="enabledCount"
        icon="lucide:user-check"
        tone="green"
        :progress="enabledPct"
        :hint="`启用率 ${enabledPct.toFixed(0)}%${total - enabledCount > 0 ? ` · 停用 ${total - enabledCount} 个` : ''}`"
      />
      <StatCard
        label="管理员"
        :value="adminCount"
        icon="lucide:shield-check"
        tone="amber"
        :hint="adminCount === 0 ? '当前没有管理员账号，请尽快补充' : '可读写全部模块'"
      />
      <StatCard
        label="只读账号"
        :value="readOnlyCount"
        icon="lucide:eye"
        tone="violet"
        :hint="readOnlyCount > 0 ? '写操作将被 RBAC 拦截（403）' : '当前没有只读账号'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list" />
          账号列表
          <em class="ipam-card-sub">{{ filtered.length }} / {{ total }}</em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <span class="ipam-field__label">自动刷新</span>
          <Switch
            v-model:checked="autoRefresh"
            size="small"
            @change="(v: any) => toggleAuto(!!v)"
          />
          <span class="ipam-hint">每 15 秒</span>
        </div>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input
            v-model:value="keyword"
            allow-clear
            placeholder="登录名 / 显示名"
            style="width: 200px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">角色</span>
          <Select
            v-model:value="roleFilter"
            style="width: 140px"
            :options="roleChoices"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">状态</span>
          <Select
            v-model:value="statusFilter"
            style="width: 128px"
            :options="STATUS_OPTIONS"
          />
        </div>
        <Button v-if="activeFilters > 0" size="small" @click="resetFilters">
          清除筛选
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">
          停用或删除当前登录账号会立即失去访问权限，已做保护
        </span>
      </div>

      <Table
        :columns="columns"
        :data-source="filtered"
        :loading="loading"
        :locale="{
          emptyText:
            activeFilters > 0
              ? '没有匹配当前筛选条件的账号'
              : '暂无账号，点击右上角「新建账号」创建',
        }"
        :pagination="false"
        :row-key="(r: UserRow) => r.id"
        :scroll="{ x: 900 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'account'">
            <div class="ipam-account">
              <span class="ipam-account__avatar" :data-self="isSelf(record.username)">
                {{ initial(record.displayName || record.username) }}
              </span>
              <div class="ipam-account__body">
                <div class="ipam-account__line">
                  <span class="ipam-account__name ipam-mono">{{ record.username }}</span>
                  <Tag v-if="isSelf(record.username)" class="m-0" color="blue">当前登录</Tag>
                </div>
                <span class="ipam-cell-meta">
                  {{ record.displayName || '未设置显示名' }}
                </span>
              </div>
            </div>
          </template>
          <template v-else-if="column.key === 'roles'">
            <div class="flex flex-wrap gap-1">
              <Tag
                v-for="r in record.roles"
                :key="r"
                class="m-0"
                :color="roleMeta(r).color"
              >
                {{ roleMeta(r).label }}
              </Tag>
            </div>
            <span class="ipam-cell-meta">
              {{ roleMeta(record.roles[0] ?? '').desc || '—' }}
            </span>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <div class="flex items-center gap-2">
              <Tooltip
                :title="isSelf(record.username) ? '不能停用当前登录账号' : ''"
              >
                <Switch
                  :checked="record.enabled"
                  :disabled="isSelf(record.username)"
                  size="small"
                  @change="(v: any) => toggle(record as UserRow, !!v)"
                />
              </Tooltip>
              <span class="ipam-hint">{{ record.enabled ? '启用' : '停用' }}</span>
            </div>
          </template>
          <template v-else-if="column.key === 'createdAt'">
            <span class="ipam-hint">{{ fmtDateTime(record.createdAt) }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="openEdit(record as UserRow)">
                <IconifyIcon icon="lucide:pencil" class="mr-1 size-3.5" />编辑
              </Button>
              <Button size="small" @click="openReset(record as UserRow)">
                <IconifyIcon icon="lucide:key-round" class="mr-1 size-3.5" />重置口令
              </Button>
              <Popconfirm
                title="确认删除该账号？"
                description="删除后该账号立即失效且不可恢复。"
                ok-text="删除"
                cancel-text="取消"
                @confirm="remove(record as UserRow)"
              >
                <Button
                  size="small"
                  danger
                  :disabled="isSelf(record.username)"
                >
                  删除
                </Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <CreateModal>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">登录名</span>
          <Input
            v-model:value="createForm.username"
            placeholder="2–64 位，字母数字与 _ . -"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">显示名（可选）</span>
          <Input v-model:value="createForm.displayName" placeholder="如 网络运维" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">初始口令</span>
          <Input.Password
            v-model:value="createForm.password"
            placeholder="至少 8 位"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">角色</span>
          <Select v-model:value="createForm.roles" :options="ROLE_OPTIONS" />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          口令仅在请求期接收并以 bcrypt 落库；只读账号的写操作会被 RBAC 拦截（403）。
        </div>
      </div>
    </CreateModal>

    <EditModal>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">登录名</span>
          <Input :value="editForm.username" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">显示名</span>
          <Input v-model:value="editForm.displayName" placeholder="如 网络运维" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">角色</span>
          <Select v-model:value="editForm.roles" :options="ROLE_OPTIONS" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">账号状态</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="editForm.enabled" size="small" />
            <span class="ipam-hint">{{ editForm.enabled ? '允许登录' : '禁止登录' }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          角色与状态变更会立即吊销该账号的存量会话；最后一个管理员不允许降级或停用。
        </div>
      </div>
    </EditModal>

    <Modal
      v-model:open="reset.open"
      :title="`重置口令 · ${reset.username}`"
      ok-text="确认重置"
      cancel-text="取消"
      @ok="doReset"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">新口令（≥8 位）</span>
          <Input.Password
            v-model:value="reset.password"
            placeholder="输入新口令"
            @press-enter="doReset"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          重置后该账号的存量登录会话会立即失效，需要使用新口令重新登录。
        </div>
      </div>
    </Modal>
  </div>
</template>
