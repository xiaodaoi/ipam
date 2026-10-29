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

// 权限域（M2-035：6 域 × read/write = 12 权限点）
const PERM_DOMAINS = [
  { icon: 'lucide:layout-dashboard', key: 'dash', label: '仪表盘' },
  { icon: 'lucide:scroll-text', key: 'logs', label: '日志中心' },
  { icon: 'lucide:network', key: 'dhcp', label: 'DHCP 服务' },
  { icon: 'lucide:globe', key: 'dns', label: 'DNS 服务' },
  { icon: 'lucide:boxes', key: 'assets', label: '资产台账' },
  { icon: 'lucide:settings', key: 'system', label: '系统管理' },
];
const ALL_PERMS = PERM_DOMAINS.flatMap((d) => [`${d.key}:read`, `${d.key}:write`]);
const READ_PERMS = PERM_DOMAINS.map((d) => `${d.key}:read`);

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
  return PERM_DOMAINS.filter((d) =>
    (r.permissions ?? []).some((p) => p.startsWith(`${d.key}:`)),
  ).map((d) => d.label);
}
function domainAccess(r: RoleRow, key: string): 'none' | 'r' | 'rw' | 'w' {
  const hasRead = (r.permissions ?? []).includes(`${key}:read`);
  const hasWrite = (r.permissions ?? []).includes(`${key}:write`);
  if (hasRead && hasWrite) return 'rw';
  if (hasRead) return 'r';
  if (hasWrite) return 'w';
  return 'none';
}
const ACCESS_TEXT: Record<string, string> = {
  none: '—',
  r: '读',
  rw: '读写',
  w: '写',
};
const ACCESS_TITLE: Record<string, string> = {
  none: '无权限',
  r: '只读',
  rw: '读写',
  w: '仅写',
};
function accessText(r: RoleRow, key: string): string {
  return ACCESS_TEXT[domainAccess(r, key)] ?? '—';
}
function accessTitle(r: RoleRow, key: string): string {
  return ACCESS_TITLE[domainAccess(r, key)] ?? '无权限';
}
function setType(key: string) {
  typeFilter.value = key as 'all' | 'builtin' | 'custom';
}

async function load() {
  loading.value = true;
  try {
    const d = await listRoles();
    rows.value = d.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '角色列表加载失败');
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
    confirmText: '创建角色',
    showCancelButton: true,
    title: '新建角色',
  });
  roleModalApi.open();
}
function openView(r: RoleRow) {
  modal.editing = r.name;
  modal.name = r.name;
  modal.perms = [...(r.permissions ?? [])];
  modal.readonly = true;
  roleModalApi.setState({
    confirmText: '关闭',
    showCancelButton: false,
    title: `查看角色：${r.name}`,
  });
  roleModalApi.open();
}
function openEdit(r: RoleRow) {
  modal.editing = r.name;
  modal.name = r.name;
  modal.perms = [...(r.permissions ?? [])];
  modal.readonly = false;
  roleModalApi.setState({
    confirmText: '保存修改',
    showCancelButton: true,
    title: `编辑角色：${r.name}`,
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
    message.warning('请填写角色名');
    return;
  }
  if (modal.perms.length === 0) {
    message.warning('请至少勾选一个权限点');
    return;
  }
  try {
    if (modal.editing) {
      await updateRole(modal.editing, { permissions: modal.perms });
      message.success(`角色「${modal.editing}」权限已更新`);
    } else {
      await createRole({ name: modal.name.trim(), permissions: modal.perms });
      message.success(`角色「${modal.name.trim()}」已创建`);
    }
    roleModalApi.close();
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  }
}

async function remove(r: RoleRow) {
  try {
    await deleteRole(r.name);
    message.success(`角色「${r.name}」已删除`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}

function resetFilters() {
  keyword.value = '';
  typeFilter.value = 'all';
}

const columns = [
  { dataIndex: 'name', key: 'name', title: '角色', minWidth: 190 },
  { dataIndex: 'builtin', key: 'builtin', title: '类型', width: 96 },
  { dataIndex: 'permissions', key: 'coverage', title: '权限覆盖', width: 190 },
  { dataIndex: 'perms', key: 'perms', title: '权限明细', minWidth: 300 },
  { dataIndex: 'op', key: 'op', title: '操作', width: 150 },
];

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:shield-check"
      title="角色管理"
      description="基于「权限域 × 读/写」定义角色；内置角色不可修改或删除，自定义角色可随时调整权限点。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:key-round" class="size-3.5" />
          {{ ALL_PERMS.length }} 个权限点 · {{ PERM_DOMAINS.length }} 个权限域
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />新建角色
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="角色总数"
        :value="total"
        icon="lucide:shield-check"
        tone="blue"
        hint="账号可同时持有多个角色，权限取并集"
      />
      <StatCard
        label="内置角色"
        :value="builtinCount"
        icon="lucide:lock"
        tone="violet"
        hint="admin / operator / auditor / user，只读"
      />
      <StatCard
        label="自定义角色"
        :value="customCount"
        icon="lucide:sparkles"
        tone="cyan"
        :hint="customCount > 0 ? '可随时编辑权限点或删除' : '尚未创建自定义角色'"
      />
      <StatCard
        label="权限点覆盖"
        :value="`${coverage} / ${ALL_PERMS.length}`"
        icon="lucide:key-round"
        tone="green"
        :progress="(coverage / ALL_PERMS.length) * 100"
        hint="全部角色合计覆盖的权限点"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-tree" />
          角色列表
          <em class="ipam-card-sub">{{ filtered.length }} / {{ total }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">读 → 域:read · 写 → 域:write</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input
            v-model:value="keyword"
            allow-clear
            placeholder="角色名"
            style="width: 200px"
          />
        </div>
        <div class="ipam-seg">
          <button
            v-for="t in [
              { key: 'all', label: '全部' },
              { key: 'builtin', label: '内置' },
              { key: 'custom', label: '自定义' },
            ]"
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
          清除筛选
        </Button>
      </div>

      <Table
        :columns="columns"
        :data-source="filtered"
        :loading="loading"
        :locale="{
          emptyText:
            keyword || typeFilter !== 'all'
              ? '没有匹配当前筛选条件的角色'
              : '暂无自定义角色，点击右上角「新建角色」创建',
        }"
        :pagination="false"
        :row-key="(r: RoleRow) => r.name"
        :scroll="{ x: 940 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-account__name ipam-mono">{{ record.name }}</span>
              <span class="ipam-cell-meta">
                可访问 {{ permLabels(record as RoleRow).length }} / {{ PERM_DOMAINS.length }} 个模块
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'builtin'">
            <Tag class="m-0" :color="record.builtin ? 'blue' : 'green'">
              {{ record.builtin ? '内置' : '自定义' }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'coverage'">
            <div class="ipam-cell-stack">
              <span class="ipam-perm-count">
                {{ (record.permissions ?? []).length }} / {{ ALL_PERMS.length }}
                <em>权限点</em>
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
                :title="`${dom.label}：${accessTitle(record as RoleRow, dom.key)}`"
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
                @click="record.builtin ? openView(record as RoleRow) : openEdit(record as RoleRow)"
              >
                <IconifyIcon
                  :icon="record.builtin ? 'lucide:eye' : 'lucide:pencil'"
                  class="mr-1 size-3.5"
                />
                {{ record.builtin ? '查看' : '编辑' }}
              </Button>
              <Popconfirm
                v-if="!record.builtin"
                title="确认删除该角色？"
                description="删除后持有该角色的账号将失去对应权限。"
                ok-text="删除"
                cancel-text="取消"
                @confirm="remove(record as RoleRow)"
              >
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <RoleModal>
      <div class="ipam-form-grid mb-3">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">角色名</span>
          <Input
            v-model:value="modal.name"
            :disabled="!!modal.editing"
            placeholder="如 network-ops"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">已选权限点</span>
          <div class="ipam-perm-selected">
            <b>{{ modal.perms.length }}</b> / {{ ALL_PERMS.length }}
            <span class="ipam-hint">个</span>
          </div>
        </div>
      </div>

      <div class="ipam-toolbar mb-3">
        <Button size="small" :disabled="modal.readonly" @click="selectAll">
          全选 {{ ALL_PERMS.length }} 项
        </Button>
        <Button size="small" :disabled="modal.readonly" @click="selectReadOnly">
          仅只读
        </Button>
        <Button size="small" :disabled="modal.readonly" @click="clearPerms">
          清空
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">勾选「编辑」会自动包含「查看」</span>
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
              全选
            </Checkbox>
          </div>
          <div class="ipam-perm-card__body">
            <Checkbox
              :checked="has(`${dom.key}:read`)"
              :disabled="modal.readonly"
              @change="(e: any) => togglePerm(`${dom.key}:read`, e.target.checked)"
            >
              查看
            </Checkbox>
            <Checkbox
              :checked="has(`${dom.key}:write`)"
              :disabled="modal.readonly"
              @change="(e: any) => togglePerm(`${dom.key}:write`, e.target.checked)"
            >
              编辑
            </Checkbox>
          </div>
        </div>
      </div>

      <div class="ipam-form-hint mt-3">
        <template v-if="modal.readonly">
          内置角色权限由系统预置，不可修改或删除；如需更细粒度授权请创建自定义角色。
        </template>
        <template v-else>
          角色创建后名称不可修改；权限变更对持有该角色的账号立即生效（下次请求即按新权限校验）。
        </template>
      </div>
    </RoleModal>
  </div>
</template>
