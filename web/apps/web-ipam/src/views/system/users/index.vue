<script setup lang="ts">
import type { SessionRow, UserRow } from '#/api/ipam';

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

import {
  createUser,
  deleteUser,
  listRoles,
  listSystemSessions,
  listUsers,
  revokeSystemSession,
  updateUser,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const ROLE_META = computed<
  Record<string, { color: string; desc: string; label: string }>
>(() => ({
  admin: {
    color: 'gold',
    desc: $t('system.users.role.adminDesc'),
    label: $t('system.users.role.admin'),
  },
  auditor: {
    color: 'purple',
    desc: $t('system.users.role.auditorDesc'),
    label: $t('system.users.role.auditor'),
  },
  operator: {
    color: 'blue',
    desc: $t('system.users.role.operatorDesc'),
    label: $t('system.users.role.operator'),
  },
  user: {
    color: 'default',
    desc: $t('system.users.role.userDesc'),
    label: $t('system.users.role.user'),
  },
}));
const roleOptions = ref<{ label: string; value: string }[]>([
  { label: 'admin', value: 'admin' },
  { label: 'user', value: 'user' },
]);
const STATUS_OPTIONS = computed(() => [
  { label: $t('system.users.status.all'), value: 'all' },
  { label: $t('system.users.status.enabled'), value: 'enabled' },
  { label: $t('system.users.status.disabled'), value: 'disabled' },
]);

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
  return new Date(t).toLocaleString(undefined, { hour12: false });
}
const isSelf = (username?: string) => !!myUsername && username === myUsername;
function roleMeta(r: string) {
  return ROLE_META.value[r] ?? { color: 'default', desc: '', label: r };
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
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.loadFailed'),
    );
  } finally {
    loading.value = false;
  }
}

// ── 筛选与统计 ──
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (
      roleFilter.value !== 'all' &&
      !(r.roles as string[]).includes(roleFilter.value)
    ) {
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
    { label: $t('system.users.roleFilterAll'), value: 'all' },
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
const enabledHint = computed(() => {
  const pct = enabledPct.value.toFixed(0);
  const disabled = total.value - enabledCount.value;
  return disabled > 0
    ? $t('system.users.stat.enabledHintMore', { pct, n: disabled })
    : $t('system.users.stat.enabledHint', { pct });
});
const activeFilters = computed(() => {
  const parts = [keyword.value.trim()];
  if (roleFilter.value === 'all') parts.push('');
  else parts.push(roleFilter.value);
  if (statusFilter.value === 'all') parts.push('');
  else parts.push(statusFilter.value);
  return parts.filter(Boolean).length;
});

// ── 创建 / 编辑 / 重置口令 ──
const createForm = reactive({
  displayName: '',
  password: '',
  roles: ['user'] as string[],
  username: '',
});
const [CreateModal, createModalApi] = useVbenModal({
  class: 'w-[560px]',
  draggable: true,
  onConfirm: () => add(),
  title: $t('system.users.modal.create'),
});

function openCreate() {
  createForm.username = '';
  createForm.displayName = '';
  createForm.password = '';
  createForm.roles = ['user'];
  createModalApi.setState({ title: $t('system.users.modal.create') });
  createModalApi.open();
}

async function add() {
  if (!/^[\w.-]{2,64}$/.test(createForm.username.trim())) {
    message.warning($t('system.users.msg.usernameRule'));
    return;
  }
  if (createForm.password.length < 10) {
    message.warning($t('system.users.msg.passwordRule'));
    return;
  }
  try {
    await createUser({
      displayName: createForm.displayName.trim() || undefined,
      password: createForm.password,
      roles: createForm.roles,
      username: createForm.username.trim(),
    });
    message.success(
      $t('system.users.msg.created', {
        name: createForm.username.trim(),
      }),
    );
    createModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.createFailed'),
    );
  }
}

const editForm = reactive({
  displayName: '',
  enabled: true,
  id: '',
  roles: ['user'] as string[],
  username: '',
});
const [EditModal, editModalApi] = useVbenModal({
  class: 'w-[520px]',
  draggable: true,
  onConfirm: () => saveEdit(),
  title: $t('system.users.modal.edit'),
});

function openEdit(row: UserRow) {
  editForm.id = row.id;
  editForm.username = row.username;
  editForm.displayName = row.displayName ?? '';
  editForm.roles = [...(row.roles as string[])];
  editForm.enabled = row.enabled;
  editModalApi.setState({ title: $t('system.users.modal.edit') });
  editModalApi.open();
}

async function saveEdit() {
  try {
    await updateUser(editForm.id, {
      displayName: editForm.displayName.trim(),
      enabled: editForm.enabled,
      roles: editForm.roles,
    });
    message.success($t('system.users.msg.updated'));
    editModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.updateFailed'),
    );
  }
}

async function toggle(row: UserRow, enabled: boolean) {
  try {
    await updateUser(row.id, { enabled });
    message.success(
      enabled
        ? $t('system.users.msg.enabled', { name: row.username })
        : $t('system.users.msg.disabled', { name: row.username }),
    );
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.users.msg.opFailed'),
    );
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
  if (reset.password.length < 10) {
    message.warning($t('system.users.msg.newPasswordRule'));
    return;
  }
  try {
    await updateUser(reset.id, { password: reset.password });
    message.success(
      $t('system.users.msg.passwordReset', { name: reset.username }),
    );
    reset.open = false;
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.resetFailed'),
    );
  }
}

async function resetTotp(row: UserRow) {
  try {
    await updateUser(row.id, { totpReset: true });
    message.success($t('system.users.msg.totpReset', { name: row.username }));
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.totpResetFailed'),
    );
  }
}

async function remove(row: UserRow) {
  try {
    await deleteUser(row.id);
    message.success($t('system.users.msg.deleted', { name: row.username }));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.deleteFailed'),
    );
  }
  await load();
}

// M6-007C：查看并强制下线指定账号的登录会话。
const sessions = reactive({
  open: false,
  username: '',
  rows: [] as SessionRow[],
  loading: false,
});

async function openSessions(row: UserRow) {
  sessions.open = true;
  sessions.username = row.username;
  sessions.rows = [];
  sessions.loading = true;
  try {
    const res = await listSystemSessions(row.id);
    sessions.rows = res.items ?? [];
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.sessionsLoadFailed'),
    );
  } finally {
    sessions.loading = false;
  }
}

async function kickSession(row: SessionRow) {
  try {
    await revokeSystemSession(row.id);
    message.success(
      $t('system.users.msg.sessionRevoked', { name: row.username }),
    );
    sessions.rows = sessions.rows.filter((s) => s.id !== row.id);
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.users.msg.revokeFailed'),
    );
  }
}

/** 从 User-Agent 提取可读设备名。 */
function deviceLabel(ua: string): string {
  if (!ua) return $t('system.users.sessions.unknownClient');
  let browser = $t('system.users.sessions.browser');
  if (/Edg\//.test(ua)) browser = 'Edge';
  else if (/Chrome\//.test(ua)) browser = 'Chrome';
  else if (/Firefox\//.test(ua)) browser = 'Firefox';
  else if (/Safari\//.test(ua)) browser = 'Safari';

  let os = '';
  if (/Windows/.test(ua)) os = 'Windows';
  else if (/Mac OS X|Macintosh/.test(ua)) os = 'macOS';
  else if (/Android/.test(ua)) os = 'Android';
  else if (/iPhone|iPad/.test(ua)) os = 'iOS';
  else if (/Linux/.test(ua)) os = 'Linux';
  return os ? `${browser} · ${os}` : browser;
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

const columns = computed(() => [
  {
    dataIndex: 'username',
    key: 'account',
    title: $t('system.users.col.account'),
    minWidth: 220,
  },
  {
    dataIndex: 'roles',
    key: 'roles',
    title: $t('system.users.col.roles'),
    width: 140,
  },
  {
    dataIndex: 'enabled',
    key: 'enabled',
    title: $t('system.users.col.enabled'),
    width: 130,
  },
  {
    dataIndex: 'createdAt',
    key: 'createdAt',
    title: $t('system.users.col.createdAt'),
    width: 170,
  },
  { dataIndex: 'op', key: 'op', title: $t('system.users.col.op'), width: 310 },
]);

async function loadRoleOptions() {
  try {
    const d = await listRoles();
    roleOptions.value = (d.items ?? []).map((r) => ({
      label: r.name,
      value: r.name,
    }));
  } catch {
    // 保留默认 admin/user
  }
}

onMounted(() => {
  void load();
  void loadRoleOptions();
  timer = setInterval(() => void load(), 15_000);
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:users"
      :title="$t('system.users.page.title')"
      :description="$t('system.users.page.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:user-check" class="size-3.5" />
          {{
            $t('system.users.header.enabledMeta', {
              enabled: enabledCount,
              total,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="reload">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.users.actions.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:user-plus" class="mr-1 size-3.5" />{{
            $t('system.users.actions.create')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.users.stat.total')"
        :value="total"
        icon="lucide:users"
        tone="blue"
        :hint="$t('system.users.stat.totalHint', { name: myUsername || '—' })"
      />
      <StatCard
        :label="$t('system.users.stat.enabled')"
        :value="enabledCount"
        icon="lucide:user-check"
        tone="green"
        :progress="enabledPct"
        :hint="enabledHint"
      />
      <StatCard
        :label="$t('system.users.stat.admin')"
        :value="adminCount"
        icon="lucide:shield-check"
        tone="amber"
        :hint="
          adminCount === 0
            ? $t('system.users.stat.adminHintEmpty')
            : $t('system.users.stat.adminHint')
        "
      />
      <StatCard
        :label="$t('system.users.stat.readonly')"
        :value="readOnlyCount"
        icon="lucide:eye"
        tone="violet"
        :hint="
          readOnlyCount > 0
            ? $t('system.users.stat.readonlyHint')
            : $t('system.users.stat.readonlyHintEmpty')
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list" />
          {{ $t('system.users.list.title') }}
          <em class="ipam-card-sub">{{ filtered.length }} / {{ total }}</em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <span class="ipam-field__label">{{
            $t('system.users.list.autoRefresh')
          }}</span>
          <Switch
            v-model:checked="autoRefresh"
            size="small"
            @change="(v: any) => toggleAuto(!!v)"
          />
          <span class="ipam-hint">{{
            $t('system.users.list.autoRefreshHint')
          }}</span>
        </div>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('system.users.filter.keyword')
          }}</span>
          <Input
            v-model:value="keyword"
            allow-clear
            :placeholder="$t('system.users.filter.keywordPlaceholder')"
            style="width: 200px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('system.users.filter.role')
          }}</span>
          <Select
            v-model:value="roleFilter"
            style="width: 140px"
            :options="roleChoices"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('system.users.filter.status')
          }}</span>
          <Select
            v-model:value="statusFilter"
            style="width: 128px"
            :options="STATUS_OPTIONS"
          />
        </div>
        <Button v-if="activeFilters > 0" size="small" @click="resetFilters">
          {{ $t('system.users.filter.clear') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">
          {{ $t('system.users.filter.hint') }}
        </span>
      </div>

      <Table
        :columns="columns"
        :data-source="filtered"
        :loading="loading"
        :locale="{
          emptyText:
            activeFilters > 0
              ? $t('system.users.empty.filtered')
              : $t('system.users.empty.none'),
        }"
        :pagination="false"
        :row-key="(r: UserRow) => r.id"
        :scroll="{ x: 900 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'account'">
            <div class="ipam-account">
              <span
                class="ipam-account__avatar"
                :data-self="isSelf(record.username)"
              >
                {{ initial(record.displayName || record.username) }}
              </span>
              <div class="ipam-account__body">
                <div class="ipam-account__line">
                  <span class="ipam-account__name ipam-mono">{{
                    record.username
                  }}</span>
                  <Tag v-if="isSelf(record.username)" class="m-0" color="blue">
                    {{ $t('system.users.tag.self') }}
                  </Tag>
                  <Tag
                    v-if="record.mustChangePassword"
                    class="m-0"
                    color="orange"
                  >
                    {{ $t('system.users.tag.mustChange') }}
                  </Tag>
                  <Tag v-if="record.totpEnabled" class="m-0" color="green">
                    2FA
                  </Tag>
                  <Tag
                    v-if="record.authSource === 'ldap'"
                    class="m-0"
                    color="geekblue"
                  >
                    {{ $t('system.users.tag.ldap') }}
                  </Tag>
                </div>
                <span class="ipam-cell-meta">
                  {{ record.displayName || $t('system.users.noDisplayName') }}
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
                :title="
                  isSelf(record.username)
                    ? $t('system.users.tooltip.selfDisable')
                    : ''
                "
              >
                <Switch
                  :checked="record.enabled"
                  :disabled="isSelf(record.username)"
                  size="small"
                  @change="(v: any) => toggle(record as UserRow, !!v)"
                />
              </Tooltip>
              <span class="ipam-hint">{{
                record.enabled
                  ? $t('system.users.enabled')
                  : $t('system.users.disabled')
              }}</span>
            </div>
          </template>
          <template v-else-if="column.key === 'createdAt'">
            <span class="ipam-hint">{{ fmtDateTime(record.createdAt) }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="openEdit(record as UserRow)">
                <IconifyIcon icon="lucide:pencil" class="mr-1 size-3.5" />{{
                  $t('system.users.action.edit')
                }}
              </Button>
              <Tooltip
                :title="
                  record.authSource === 'ldap'
                    ? $t('system.users.tooltip.ldapPassword')
                    : ''
                "
              >
                <Button
                  size="small"
                  :disabled="record.authSource === 'ldap'"
                  @click="openReset(record as UserRow)"
                >
                  <IconifyIcon
                    icon="lucide:key-round"
                    class="mr-1 size-3.5"
                  />{{ $t('system.users.action.resetPassword') }}
                </Button>
              </Tooltip>
              <Popconfirm
                v-if="record.totpEnabled"
                :title="$t('system.users.confirm.totpTitle')"
                :description="$t('system.users.confirm.totpDesc')"
                :ok-text="$t('system.users.action.reset')"
                :cancel-text="$t('system.users.action.cancel')"
                @confirm="resetTotp(record as UserRow)"
              >
                <Button size="small">
                  <IconifyIcon
                    icon="lucide:shield-off"
                    class="mr-1 size-3.5"
                  />{{ $t('system.users.action.resetTotp') }}
                </Button>
              </Popconfirm>
              <Button size="small" @click="openSessions(record as UserRow)">
                <IconifyIcon
                  icon="lucide:monitor-smartphone"
                  class="mr-1 size-3.5"
                />{{ $t('system.users.action.sessions') }}
              </Button>
              <Popconfirm
                :title="$t('system.users.confirm.deleteTitle')"
                :description="$t('system.users.confirm.deleteDesc')"
                :ok-text="$t('system.users.action.delete')"
                :cancel-text="$t('system.users.action.cancel')"
                @confirm="remove(record as UserRow)"
              >
                <Button size="small" danger :disabled="isSelf(record.username)">
                  {{ $t('system.users.action.delete') }}
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
          <span class="ipam-form-field__label">{{
            $t('system.users.form.username')
          }}</span>
          <Input
            v-model:value="createForm.username"
            :placeholder="$t('system.users.form.usernamePlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.displayName')
          }}</span>
          <Input
            v-model:value="createForm.displayName"
            :placeholder="$t('system.users.form.displayNamePlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.initialPassword')
          }}</span>
          <Input.Password
            v-model:value="createForm.password"
            :placeholder="$t('system.users.form.initialPasswordPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.role')
          }}</span>
          <Select
            v-model:value="createForm.roles"
            mode="multiple"
            :options="roleOptions"
            :placeholder="$t('system.users.form.rolePlaceholder')"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.users.form.createHint') }}
        </div>
      </div>
    </CreateModal>

    <EditModal>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.username')
          }}</span>
          <Input :value="editForm.username" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.displayNameEdit')
          }}</span>
          <Input
            v-model:value="editForm.displayName"
            :placeholder="$t('system.users.form.displayNamePlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.role')
          }}</span>
          <Select
            v-model:value="editForm.roles"
            mode="multiple"
            :options="roleOptions"
            :placeholder="$t('system.users.form.rolePlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.users.form.accountStatus')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="editForm.enabled" size="small" />
            <span class="ipam-hint">{{
              editForm.enabled
                ? $t('system.users.form.allowLogin')
                : $t('system.users.form.forbidLogin')
            }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.users.form.editHint') }}
        </div>
      </div>
    </EditModal>

    <Modal
      v-model:open="reset.open"
      :title="$t('system.users.reset.title', { name: reset.username })"
      :ok-text="$t('system.users.reset.ok')"
      :cancel-text="$t('system.users.action.cancel')"
      @ok="doReset"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.users.reset.label')
          }}</span>
          <Input.Password
            v-model:value="reset.password"
            :placeholder="$t('system.users.reset.placeholder')"
            @press-enter="doReset"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.users.reset.hint') }}
        </div>
      </div>
    </Modal>

    <Modal
      v-model:open="sessions.open"
      :footer="null"
      :title="$t('system.users.sessions.title', { name: sessions.username })"
      :width="640"
    >
      <div v-if="sessions.loading" class="ipam-hint py-4">
        {{ $t('system.users.sessions.loading') }}
      </div>
      <div v-else-if="sessions.rows.length === 0" class="ipam-hint py-4">
        {{ $t('system.users.sessions.empty') }}
      </div>
      <div v-else class="flex flex-col gap-2">
        <div
          v-for="s in sessions.rows"
          :key="s.id"
          class="flex flex-wrap items-center justify-between gap-3 rounded border border-gray-200 px-3 py-2 dark:border-gray-700"
        >
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <span class="font-medium">{{ deviceLabel(s.userAgent) }}</span>
              <Tag v-if="s.current" class="m-0" color="blue">
                {{ $t('system.users.sessions.current') }}
              </Tag>
            </div>
            <div class="ipam-hint break-all">
              {{
                $t('system.users.sessions.meta', {
                  ip: s.ip || $t('system.users.sessions.unknownIp'),
                  login: fmtDateTime(s.createdAt),
                  last: fmtDateTime(s.lastSeenAt),
                })
              }}
            </div>
          </div>
          <Popconfirm
            :title="$t('system.users.sessions.confirmTitle')"
            :description="$t('system.users.sessions.confirmDesc')"
            :ok-text="$t('system.users.sessions.ok')"
            :cancel-text="$t('system.users.sessions.cancel')"
            @confirm="kickSession(s)"
          >
            <Button danger size="small">
              {{ $t('system.users.sessions.kick') }}
            </Button>
          </Popconfirm>
        </div>
      </div>
    </Modal>
  </div>
</template>
