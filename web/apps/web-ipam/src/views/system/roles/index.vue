<script setup lang="ts">
import type { RoleRow } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Checkbox,
  Input,
  message,
  Popconfirm,
  Table,
  Tag,
} from 'ant-design-vue';

import { createRole, deleteRole, listRoles, updateRole } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

// 权限域（M2-035：6 域 × read/write = 12 权限点）
const DOMAIN_DEFS = [
  { icon: 'lucide:layout-dashboard', key: 'dash' },
  { icon: 'lucide:scroll-text', key: 'logs' },
  { icon: 'lucide:network', key: 'dhcp' },
  { icon: 'lucide:globe', key: 'dns' },
  { icon: 'lucide:boxes', key: 'assets' },
  { icon: 'lucide:settings', key: 'system' },
];
const PERM_DOMAINS = computed(() =>
  DOMAIN_DEFS.map((d) => ({
    ...d,
    label: $t(`system.roles.domain.${d.key}`),
  })),
);
const ALL_PERMS = DOMAIN_DEFS.flatMap((d) => [
  `${d.key}:read`,
  `${d.key}:write`,
]);
const READ_PERMS = DOMAIN_DEFS.map((d) => `${d.key}:read`);

const rows = ref<RoleRow[]>([]);
const loading = ref(false);
const keyword = ref('');
const typeFilter = ref<'all' | 'builtin' | 'custom'>('all');

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (typeFilter.value === 'builtin' && !r.builtin) return false;
    if (typeFilter.value === 'custom' && r.builtin) return false;
    return !kw || r.name.toLowerCase().includes(kw);
  });
});
const total = computed(() => rows.value.length);
const builtinCount = computed(() => rows.value.filter((r) => r.builtin).length);
const customCount = computed(() => total.value - builtinCount.value);
const coverage = computed(() => {
  const set = new Set<string>();
  for (const r of rows.value) for (const p of r.permissions ?? []) set.add(p);
  return set.size;
});

function permLabels(r: RoleRow): string[] {
  return PERM_DOMAINS.value
    .filter((d) => (r.permissions ?? []).some((p) => p.startsWith(`${d.key}:`)))
    .map((d) => d.label);
}
function domainAccess(r: RoleRow, key: string): 'none' | 'r' | 'rw' | 'w' {
  const hasRead = (r.permissions ?? []).includes(`${key}:read`);
  const hasWrite = (r.permissions ?? []).includes(`${key}:write`);
  if (hasRead && hasWrite) return 'rw';
  if (hasRead) return 'r';
  if (hasWrite) return 'w';
  return 'none';
}
const ACCESS_TEXT = computed<Record<string, string>>(() => ({
  none: '—',
  r: $t('system.roles.access.r'),
  rw: $t('system.roles.access.rw'),
  w: $t('system.roles.access.w'),
}));
const ACCESS_TITLE = computed<Record<string, string>>(() => ({
  none: $t('system.roles.accessTitle.none'),
  r: $t('system.roles.accessTitle.r'),
  rw: $t('system.roles.accessTitle.rw'),
  w: $t('system.roles.accessTitle.w'),
}));
function accessText(r: RoleRow, key: string): string {
  return ACCESS_TEXT.value[domainAccess(r, key)] ?? '—';
}
function accessTitle(r: RoleRow, key: string): string {
  return (
    ACCESS_TITLE.value[domainAccess(r, key)] ??
    $t('system.roles.accessTitle.none')
  );
}
function setType(key: string) {
  typeFilter.value = key as 'all' | 'builtin' | 'custom';
}

const TYPE_TABS = computed(() => [
  { key: 'all', label: $t('system.roles.filter.all') },
  { key: 'builtin', label: $t('system.roles.filter.builtin') },
  { key: 'custom', label: $t('system.roles.filter.custom') },
]);

async function load() {
  loading.value = true;
  try {
    const d = await listRoles();
    rows.value = d.items ?? [];
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.roles.msg.loadFailed'),
    );
  } finally {
    loading.value = false;
  }
}

// ── 弹窗：新建 / 查看 / 编辑 ──
const modal = reactive({
  editing: '',
  name: '',
  perms: [] as string[],
  readonly: false,
});
const [RoleModal, roleModalApi] = useVbenModal({
  class: 'w-[720px]',
  draggable: true,
  onConfirm: () => save(),
});

function openCreate() {
  modal.editing = '';
  modal.name = '';
  modal.perms = [];
  modal.readonly = false;
  roleModalApi.setState({
    confirmText: $t('system.roles.modal.createOk'),
    showCancelButton: true,
    title: $t('system.roles.modal.createTitle'),
  });
  roleModalApi.open();
}
function openView(r: RoleRow) {
  modal.editing = r.name;
  modal.name = r.name;
  modal.perms = [...(r.permissions ?? [])];
  modal.readonly = true;
  roleModalApi.setState({
    confirmText: $t('system.roles.modal.close'),
    showCancelButton: false,
    title: $t('system.roles.modal.viewTitle', { name: r.name }),
  });
  roleModalApi.open();
}
function openEdit(r: RoleRow) {
  modal.editing = r.name;
  modal.name = r.name;
  modal.perms = [...(r.permissions ?? [])];
  modal.readonly = false;
  roleModalApi.setState({
    confirmText: $t('system.roles.modal.editOk'),
    showCancelButton: true,
    title: $t('system.roles.modal.editTitle', { name: r.name }),
  });
  roleModalApi.open();
}

function has(p: string) {
  return modal.perms.includes(p);
}
/** read/write 联动：勾写自动包含读；取消读自动取消写 */
function togglePerm(p: string, checked: boolean) {
  const [domain, kind] = p.split(':') as [string, 'read' | 'write'];
  const readPerm = `${domain}:read`;
  const writePerm = `${domain}:write`;
  const set = new Set(modal.perms);
  if (checked) {
    set.add(p);
    if (kind === 'write') set.add(readPerm);
  } else {
    set.delete(p);
    if (kind === 'read') set.delete(writePerm);
  }
  modal.perms = ALL_PERMS.filter((x) => set.has(x));
}
function checkDomain(key: string, checked: boolean) {
  const set = new Set(modal.perms);
  for (const kind of ['read', 'write']) {
    const p = `${key}:${kind}`;
    if (checked) set.add(p);
    else set.delete(p);
  }
  modal.perms = ALL_PERMS.filter((x) => set.has(x));
}
function selectAll() {
  modal.perms = [...ALL_PERMS];
}
function selectReadOnly() {
  modal.perms = [...READ_PERMS];
}
function clearPerms() {
  modal.perms = [];
}

async function save() {
  if (modal.readonly) {
    roleModalApi.close();
    return;
  }
  if (!modal.editing && !modal.name.trim()) {
    message.warning($t('system.roles.msg.nameRequired'));
    return;
  }
  if (modal.perms.length === 0) {
    message.warning($t('system.roles.msg.permRequired'));
    return;
  }
  try {
    if (modal.editing) {
      await updateRole(modal.editing, { permissions: modal.perms });
      message.success($t('system.roles.msg.updated', { name: modal.editing }));
    } else {
      await createRole({ name: modal.name.trim(), permissions: modal.perms });
      message.success(
        $t('system.roles.msg.created', { name: modal.name.trim() }),
      );
    }
    roleModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.roles.msg.saveFailed'),
    );
  }
}

async function remove(r: RoleRow) {
  try {
    await deleteRole(r.name);
    message.success($t('system.roles.msg.deleted', { name: r.name }));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.roles.msg.deleteFailed'),
    );
  }
  await load();
}

function resetFilters() {
  keyword.value = '';
  typeFilter.value = 'all';
}

const columns = computed(() => [
  {
    dataIndex: 'name',
    key: 'name',
    title: $t('system.roles.col.name'),
    minWidth: 190,
  },
  {
    dataIndex: 'builtin',
    key: 'builtin',
    title: $t('system.roles.col.builtin'),
    width: 96,
  },
  {
    dataIndex: 'permissions',
    key: 'coverage',
    title: $t('system.roles.col.coverage'),
    width: 190,
  },
  {
    dataIndex: 'perms',
    key: 'perms',
    title: $t('system.roles.col.perms'),
    minWidth: 300,
  },
  { dataIndex: 'op', key: 'op', title: $t('system.roles.col.op'), width: 150 },
]);

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:shield-check"
      :title="$t('system.roles.page.title')"
      :description="$t('system.roles.page.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:key-round" class="size-3.5" />
          {{
            $t('system.roles.header.meta', {
              perms: ALL_PERMS.length,
              domains: PERM_DOMAINS.length,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.roles.actions.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />{{
            $t('system.roles.actions.create')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.roles.stat.total')"
        :value="total"
        icon="lucide:shield-check"
        tone="blue"
        :hint="$t('system.roles.stat.totalHint')"
      />
      <StatCard
        :label="$t('system.roles.stat.builtin')"
        :value="builtinCount"
        icon="lucide:lock"
        tone="violet"
        :hint="$t('system.roles.stat.builtinHint')"
      />
      <StatCard
        :label="$t('system.roles.stat.custom')"
        :value="customCount"
        icon="lucide:sparkles"
        tone="cyan"
        :hint="
          customCount > 0
            ? $t('system.roles.stat.customHint')
            : $t('system.roles.stat.customHintEmpty')
        "
      />
      <StatCard
        :label="$t('system.roles.stat.coverage')"
        :value="`${coverage} / ${ALL_PERMS.length}`"
        icon="lucide:key-round"
        tone="green"
        :progress="(coverage / ALL_PERMS.length) * 100"
        :hint="$t('system.roles.stat.coverageHint')"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-tree" />
          {{ $t('system.roles.list.title') }}
          <em class="ipam-card-sub">{{ filtered.length }} / {{ total }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.roles.list.rule') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('system.roles.filter.keyword')
          }}</span>
          <Input
            v-model:value="keyword"
            allow-clear
            :placeholder="$t('system.roles.filter.keywordPlaceholder')"
            style="width: 200px"
          />
        </div>
        <div class="ipam-seg">
          <button
            v-for="t in TYPE_TABS"
            :key="t.key"
            type="button"
            :data-active="typeFilter === t.key"
            @click="setType(t.key)"
          >
            {{ t.label }}
          </button>
        </div>
        <Button
          v-if="keyword || typeFilter !== 'all'"
          size="small"
          @click="resetFilters"
        >
          {{ $t('system.roles.filter.clear') }}
        </Button>
      </div>

      <Table
        :columns="columns"
        :data-source="filtered"
        :loading="loading"
        :locale="{
          emptyText:
            keyword || typeFilter !== 'all'
              ? $t('system.roles.empty.filtered')
              : $t('system.roles.empty.none'),
        }"
        :pagination="false"
        :row-key="(r: RoleRow) => r.name"
        :scroll="{ x: 940 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-account__name ipam-mono">{{
                record.name
              }}</span>
              <span class="ipam-cell-meta">
                {{
                  $t('system.roles.cell.access', {
                    n: permLabels(record as RoleRow).length,
                    total: PERM_DOMAINS.length,
                  })
                }}
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'builtin'">
            <Tag class="m-0" :color="record.builtin ? 'blue' : 'green'">
              {{
                record.builtin
                  ? $t('system.roles.cell.builtin')
                  : $t('system.roles.cell.custom')
              }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'coverage'">
            <div class="ipam-cell-stack">
              <span class="ipam-perm-count">
                {{ (record.permissions ?? []).length }} / {{ ALL_PERMS.length }}
                <em>{{ $t('system.roles.cell.permCount') }}</em>
              </span>
              <div class="ipam-progress ipam-perm-bar">
                <i
                  :style="{
                    width: `${((record.permissions ?? []).length / ALL_PERMS.length) * 100}%`,
                  }"
                ></i>
              </div>
            </div>
          </template>
          <template v-else-if="column.key === 'perms'">
            <div class="ipam-perm-chips">
              <span
                v-for="dom in PERM_DOMAINS"
                :key="dom.key"
                class="ipam-perm-chip"
                :data-access="domainAccess(record as RoleRow, dom.key)"
                :title="
                  $t('system.roles.cell.chipTitle', {
                    domain: dom.label,
                    access: accessTitle(record as RoleRow, dom.key),
                  })
                "
              >
                {{ dom.label }}
                <b>{{ accessText(record as RoleRow, dom.key) }}</b>
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button
                size="small"
                @click="
                  record.builtin
                    ? openView(record as RoleRow)
                    : openEdit(record as RoleRow)
                "
              >
                <IconifyIcon
                  :icon="record.builtin ? 'lucide:eye' : 'lucide:pencil'"
                  class="mr-1 size-3.5"
                />
                {{
                  record.builtin
                    ? $t('system.roles.action.view')
                    : $t('system.roles.action.edit')
                }}
              </Button>
              <Popconfirm
                v-if="!record.builtin"
                :title="$t('system.roles.confirm.deleteTitle')"
                :description="$t('system.roles.confirm.deleteDesc')"
                :ok-text="$t('system.roles.action.delete')"
                :cancel-text="$t('system.roles.action.cancel')"
                @confirm="remove(record as RoleRow)"
              >
                <Button size="small" danger>
                  {{ $t('system.roles.action.delete') }}
                </Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <RoleModal>
      <div class="ipam-form-grid mb-3">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.roles.modal.name')
          }}</span>
          <Input
            v-model:value="modal.name"
            :disabled="!!modal.editing"
            :placeholder="$t('system.roles.modal.namePlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.roles.modal.selected')
          }}</span>
          <div class="ipam-perm-selected">
            <b>{{ modal.perms.length }}</b> / {{ ALL_PERMS.length }}
            <span class="ipam-hint">{{
              $t('system.roles.modal.selectedUnit')
            }}</span>
          </div>
        </div>
      </div>

      <div class="ipam-toolbar mb-3">
        <Button size="small" :disabled="modal.readonly" @click="selectAll">
          {{ $t('system.roles.modal.selectAll', { n: ALL_PERMS.length }) }}
        </Button>
        <Button size="small" :disabled="modal.readonly" @click="selectReadOnly">
          {{ $t('system.roles.modal.readOnly') }}
        </Button>
        <Button size="small" :disabled="modal.readonly" @click="clearPerms">
          {{ $t('system.roles.modal.clear') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('system.roles.modal.hintSelect') }}</span>
      </div>

      <div class="ipam-perm-grid">
        <div v-for="dom in PERM_DOMAINS" :key="dom.key" class="ipam-perm-card">
          <div class="ipam-perm-card__head">
            <span class="ipam-perm-card__title">
              <IconifyIcon :icon="dom.icon" class="size-3.5" />{{ dom.label }}
            </span>
            <Checkbox
              :checked="has(`${dom.key}:read`) && has(`${dom.key}:write`)"
              :disabled="modal.readonly"
              @change="(e: any) => checkDomain(dom.key, e.target.checked)"
            >
              {{ $t('system.roles.modal.all') }}
            </Checkbox>
          </div>
          <div class="ipam-perm-card__body">
            <Checkbox
              :checked="has(`${dom.key}:read`)"
              :disabled="modal.readonly"
              @change="
                (e: any) => togglePerm(`${dom.key}:read`, e.target.checked)
              "
            >
              {{ $t('system.roles.modal.read') }}
            </Checkbox>
            <Checkbox
              :checked="has(`${dom.key}:write`)"
              :disabled="modal.readonly"
              @change="
                (e: any) => togglePerm(`${dom.key}:write`, e.target.checked)
              "
            >
              {{ $t('system.roles.modal.write') }}
            </Checkbox>
          </div>
        </div>
      </div>

      <div class="ipam-form-hint mt-3">
        <template v-if="modal.readonly">
          {{ $t('system.roles.modal.readonlyHint') }}
        </template>
        <template v-else>
          {{ $t('system.roles.modal.editableHint') }}
        </template>
      </div>
    </RoleModal>
  </div>
</template>
