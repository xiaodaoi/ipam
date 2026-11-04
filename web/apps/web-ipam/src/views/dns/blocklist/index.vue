<script setup lang="ts">
import type { Blocklist, BlocklistEntryRow, PolicyGroupRow } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { useVbenDrawer, useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Modal, Popconfirm, Select, Switch, Table, Tag } from 'ant-design-vue';

import { addBlocklistEntry, compilePolicyGroup, createBlocklist, createPolicyGroup, deleteBlocklist, deleteBlocklistEntry, exportBlocklistCsv, importBlocklistEntries, installBuiltinPack, listBlocklistEntries, listBlocklists, listBlocklistVersions, listBuiltinPacks, listPolicyGroups, rollbackBlocklist, syncBlocklist } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const showTotal = (t: number) => `共 ${t} 条`;

const rows = ref<Blocklist[]>([]);
const loading = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

const entries = ref<BlocklistEntryRow[]>([]);
const eLoading = ref(false);
const selId = ref('');
const selName = ref('');
const eForm = ref<{ action: 'drop' | 'nxdomain' | 'redirect' | 'tcp_only'; pattern: string; triggerType: 'qname' | 'response_ip' }>({ pattern: '', triggerType: 'qname', action: 'nxdomain' });
const cForm = ref<{ kind: 'builtin' | 'custom' | 'feed'; name: string; syncUrl: string }>({ name: '', kind: 'custom', syncUrl: '' });
const pgRows = ref<PolicyGroupRow[]>([]);
const pgLoading = ref(false);
const pgForm = ref<{
  cidrs: string;
  listIds: string[];
  name: string;
  scheduleDays: number[];
  scheduleEnd: string;
  scheduleStart: string;
  viewName: string;
}>({ name: '', viewName: 'recursor', cidrs: '', listIds: [], scheduleDays: [], scheduleStart: '', scheduleEnd: '' });
const pgResult = ref<null | Record<string, unknown>>(null);
const syncingId = ref('');

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
  if (!id) return;
  syncingId.value = id;
  try {
    await syncBlocklist(id);
    message.success('同步任务已提交');
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '同步失败');
  } finally {
    syncingId.value = '';
  }
}
async function removeList(id: string) {
  try {
    await deleteBlocklist(id);
    message.success('名单已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}

const [CreateModal, createModalApi] = useVbenModal({ draggable: true, title: '新建名单', confirmText: '创建名单', onConfirm: () => createList() });
function openCreate() {
  cForm.value = { name: '', kind: 'custom', syncUrl: '' };
  createModalApi.open();
}
async function createList() {
  if (!cForm.value.name) {
    message.warning('名单名称必填');
    return;
  }
  if (cForm.value.kind === 'feed' && !cForm.value.syncUrl) {
    message.warning('订阅名单需填写订阅 URL');
    return;
  }
  try {
    await createBlocklist({ name: cForm.value.name, kind: cForm.value.kind, syncUrl: cForm.value.syncUrl || undefined });
    message.success('名单已创建');
    cForm.value = { name: '', kind: 'custom', syncUrl: '' };
    createModalApi.close();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '创建失败');
  }
  await load();
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
const [PgModal, pgModalApi] = useVbenModal({ draggable: true, title: '新建策略分组', confirmText: '创建分组', onConfirm: () => createPg() });
function openCreatePg() {
  pgForm.value = {
    name: '',
    viewName: 'recursor',
    cidrs: '',
    listIds: [],
    scheduleDays: [],
    scheduleStart: '',
    scheduleEnd: '',
  };
  pgModalApi.open();
}
async function createPg() {
  if (!pgForm.value.name) {
    message.warning('分组名称必填');
    return;
  }
  try {
    const schedule =
      pgForm.value.scheduleStart && pgForm.value.scheduleEnd
        ? {
            days: pgForm.value.scheduleDays,
            start: pgForm.value.scheduleStart,
            end: pgForm.value.scheduleEnd,
          }
        : undefined;
    await createPolicyGroup({
      name: pgForm.value.name,
      viewName: pgForm.value.viewName || 'recursor',
      cidrs: pgForm.value.cidrs.split(',').map((s) => s.trim()).filter(Boolean),
      listIds: pgForm.value.listIds,
      schedule,
    });
    message.success('策略分组已创建');
    pgForm.value = {
      name: '',
      viewName: 'recursor',
      cidrs: '',
      listIds: [],
      scheduleDays: [],
      scheduleStart: '',
      scheduleEnd: '',
    };
    pgModalApi.close();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '创建失败');
  }
  await loadPolicyGroups();
}
async function compilePg(id: string) {
  try {
    pgResult.value = await compilePolicyGroup(id);
    message.success('RPZ 编译完成');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '编译失败');
  }
}

// ── 条目抽屉（M2-046：条目在抽屉中展示 + 客户端分页）──
const [EntriesDrawer, entriesDrawerApi] = useVbenDrawer({
  class: 'w-[780px]',
  footer: false,
  title: '名单条目',
});
async function openEntries(r: Blocklist) {
  selId.value = r.id;
  selName.value = r.name;
  entries.value = [];
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
  if (!eForm.value.pattern) {
    message.warning('请填写匹配规则');
    return;
  }
  try {
    await addBlocklistEntry(selId.value, { pattern: eForm.value.pattern, triggerType: eForm.value.triggerType, action: eForm.value.action });
    message.success('条目已添加');
    eForm.value = { pattern: '', triggerType: 'qname', action: 'nxdomain' };
    await loadEntries();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '添加失败');
  }
}
async function removeEntry(listId: string, pattern: string) {
  try {
    await deleteBlocklistEntry(listId, pattern);
    message.success('条目已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await loadEntries();
}

// ── CSV 导入导出 / 版本回滚 / 内置分类库（M6-023）──
const csvOpen = ref(false);
const csvContent = ref('');
const csvReplace = ref(false);
const csvImporting = ref(false);
function openCsv() {
  if (!selId.value) return;
  csvContent.value = '';
  csvReplace.value = false;
  csvOpen.value = true;
}
async function submitCsv() {
  if (!csvContent.value.trim()) {
    message.warning('请粘贴 CSV 内容');
    return;
  }
  csvImporting.value = true;
  try {
    const r = await importBlocklistEntries(selId.value, csvContent.value, csvReplace.value);
    message.success(`导入完成：新增 ${r.created} 条，跳过 ${r.skipped} 条，共 ${r.total} 条`);
    if (r.errors?.length) message.warning(r.errors.slice(0, 3).join('；'));
    csvOpen.value = false;
    await Promise.all([loadEntries(), load()]);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导入失败');
  } finally {
    csvImporting.value = false;
  }
}
async function exportCsv(id?: string) {
  const listId = id || selId.value;
  if (!listId) return;
  try {
    await exportBlocklistCsv(listId);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导出失败');
  }
}

const versionsOpen = ref(false);
const versionsLoading = ref(false);
const versions = ref<{ createdAt: string; entries: number; version: number; }[]>([]);
async function openVersions(r: Blocklist) {
  selId.value = r.id;
  selName.value = r.name;
  versions.value = [];
  versionsOpen.value = true;
  versionsLoading.value = true;
  try {
    const data = await listBlocklistVersions(r.id);
    versions.value = data.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '版本加载失败');
  } finally {
    versionsLoading.value = false;
  }
}
async function doRollback(version: number) {
  try {
    await rollbackBlocklist(selId.value, version);
    message.success(`已回滚到 v${version}（当前版本继续递增）`);
    versionsOpen.value = false;
    await Promise.all([load(), loadEntries()]);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '回滚失败');
  }
}

const builtinOpen = ref(false);
const builtinLoading = ref(false);
const packs = ref<{ categories?: string[]; description?: string; entryCount: number; key: string; name: string; }[]>([]);
async function openBuiltin() {
  builtinLoading.value = true;
  builtinOpen.value = true;
  try {
    const data = await listBuiltinPacks();
    packs.value = data.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '内置库加载失败');
  } finally {
    builtinLoading.value = false;
  }
}
async function installPack(key: string) {
  try {
    const b = await installBuiltinPack(key);
    message.success(`已安装「${b.name}」（${b.version ? `v${b.version}` : ''}）`);
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '安装失败');
  }
}

const WEEK_OPTIONS = [
  { label: '周日', value: 0 },
  { label: '周一', value: 1 },
  { label: '周二', value: 2 },
  { label: '周三', value: 3 },
  { label: '周四', value: 4 },
  { label: '周五', value: 5 },
  { label: '周六', value: 6 },
];

const KIND_TEXT: Record<string, string> = { builtin: '内置', custom: '自定义', feed: '订阅' };
const KIND_COLOR: Record<string, string> = { builtin: 'default', custom: 'blue', feed: 'purple' };
const ACTION_TEXT: Record<string, string> = { drop: '丢弃', nxdomain: 'NXDOMAIN', redirect: '重定向', tcp_only: '仅 TCP' };
function fmtSync(v?: string) {
  if (!v) return '—';
  const date = new Date(v);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1970) return '—';
  return date.toLocaleString('zh-CN', { hour12: false });
}

// ── 指标与筛选 ──
const stats = computed(() => {
  const feeds = rows.value.filter((r) => r.kind === 'feed').length;
  const builtin = rows.value.filter((r) => r.kind === 'builtin').length;
  const custom = rows.value.filter((r) => r.kind === 'custom').length;
  return {
    builtin,
    custom,
    feeds,
    groups: pgRows.value.length,
    total: rows.value.length,
  };
});

const listKeyword = ref('');
const listKind = ref<'all' | Blocklist['kind']>('all');
const kindOptions = [
  { label: '全部类型', value: 'all' },
  { label: '内置名单', value: 'builtin' },
  { label: '自定义', value: 'custom' },
  { label: '订阅源', value: 'feed' },
];
const filteredLists = computed(() => {
  const kw = listKeyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (listKind.value !== 'all' && r.kind !== listKind.value) return false;
    return !kw || `${r.name} ${r.syncUrl ?? ''}`.toLowerCase().includes(kw);
  });
});
const pgKeyword = ref('');
const filteredGroups = computed(() => {
  const kw = pgKeyword.value.trim().toLowerCase();
  if (!kw) return pgRows.value;
  return pgRows.value.filter((r) =>
    `${r.name} ${r.viewName} ${(r.cidrs ?? []).join(' ')}`.toLowerCase().includes(kw),
  );
});
const pgListOptions = computed(() => rows.value.map((r) => ({
  label: `${r.name}（${KIND_TEXT[r.kind] ?? r.kind}）`,
  value: r.id,
})));

onMounted(() => {
  void loadPolicyGroups();
  void load();
  timer = setInterval(load, 30_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:shield-ban"
      title="封禁管控"
      description="维护封禁名单与策略分组，经 RPZ 下发到 resolver 实现域名与应答 IP 拦截。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:list-filter" class="size-3.5" />
          {{ stats.total }} 个名单 · {{ stats.groups }} 个分组
        </span>
        <Button size="small" :loading="loading" @click="load(); loadPolicyGroups()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1" />新建名单
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="封禁名单"
        :value="stats.total"
        unit="个"
        icon="lucide:list-filter"
        tone="blue"
        :hint="`自定义 ${stats.custom} · 订阅 ${stats.feeds}`"
      />
      <StatCard
        label="订阅名单"
        :value="stats.feeds"
        unit="个"
        icon="lucide:rss"
        tone="violet"
        :hint="stats.feeds > 0 ? '每 30 秒检查一次同步' : '尚未接入订阅源'"
      />
      <StatCard
        label="策略分组"
        :value="stats.groups"
        unit="个"
        icon="lucide:network"
        tone="cyan"
        :hint="stats.groups > 0 ? '按 view 应用 RPZ' : '尚未创建分组'"
      />
      <StatCard
        label="内置名单"
        :value="stats.builtin"
        unit="个"
        icon="lucide:lock"
        tone="green"
        :hint="stats.builtin > 0 ? '内置名单不可删除' : '暂无内置名单'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-ban" />
          封禁名单库
          <em class="ipam-card-sub">{{ filteredLists.length }} / {{ rows.length }} 个</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">订阅源每 30 秒检查同步 · 失败保留旧版本</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input v-model:value="listKeyword" allow-clear placeholder="名单名称 / 订阅地址" style="width: 240px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">类型</span>
          <Select v-model:value="listKind" :options="kindOptions" style="width: 140px" />
        </div>
        <Button size="small" @click="listKeyword = ''; listKind = 'all'">重置</Button>
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="openBuiltin">
          <IconifyIcon icon="lucide:package" class="mr-1" />内置分类库
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1" />新建名单
        </Button>
      </div>

      <Table
        :data-source="filteredLists"
        :loading="loading"
        :columns="[
          { title: '名单（名称 / 订阅地址）', dataIndex: 'name', minWidth: 280 },
          { title: '类型', dataIndex: 'kind', width: 100 },
          { title: '同步信息', dataIndex: 'lastSync', minWidth: 200 },
          { title: '操作', key: 'op', width: 240 },
        ]"
        :locale="{ emptyText: listKeyword || listKind !== 'all' ? '没有匹配的封禁名单' : '暂无封禁名单，点击右上角「新建名单」创建' }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono">{{ record.syncUrl || (record.kind === 'builtin' ? '随产品内置，不可编辑' : '本地维护，无订阅地址') }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'kind'">
            <Tag class="m-0" :color="KIND_COLOR[record.kind] ?? 'default'">
              {{ KIND_TEXT[record.kind] ?? record.kind }}
            </Tag>
          </template>
          <template v-else-if="column.dataIndex === 'lastSync'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title" style="font-weight: 400">{{ fmtSync(record.lastSync) }}</span>
              <span class="ipam-cell-meta">版本 v{{ record.version }}</span>
            </div>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="openEntries(record as Blocklist)">条目</Button>
              <Button size="small" @click="exportCsv(record.id)">导出</Button>
              <Button size="small" @click="openVersions(record as Blocklist)">版本</Button>
              <Button
                v-if="record.kind === 'feed'"
                size="small"
                :loading="syncingId === record.id"
                @click="sync(record.id)"
              >
                立即同步
              </Button>
              <Popconfirm
                v-if="record.kind !== 'builtin'"
                title="确认删除该名单？"
                description="名单及其条目将一并删除，不可恢复。"
                ok-text="删除"
                cancel-text="取消"
                @confirm="removeList(record.id)"
              >
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>

      <div class="ipam-note mt-3">
        <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        <span>内置名单不可删除；订阅源同步前自动保存版本快照，可在「版本」中一键回滚；
          提示页文案与 IP 在「安全与诊断」页配置。</span>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:network" />
          策略分组
          <em class="ipam-card-sub">{{ filteredGroups.length }} / {{ pgRows.length }} 个</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">view 级 RPZ 应用 · 白单优先</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input v-model:value="pgKeyword" allow-clear placeholder="分组名称 / view / CIDR" style="width: 240px" />
        </div>
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="openCreatePg">
          <IconifyIcon icon="lucide:plus" class="mr-1" />创建分组
        </Button>
      </div>

      <Table
        :data-source="filteredGroups"
        :loading="pgLoading"
        :columns="[
          { title: '分组（名称 / view）', dataIndex: 'name', minWidth: 220 },
          { title: '来源网段', dataIndex: 'cidrs', minWidth: 240 },
          { title: '名单数', dataIndex: 'listIds', width: 100 },
          { title: '操作', key: 'op', width: 120 },
        ]"
        :locale="{ emptyText: pgKeyword ? '没有匹配的策略分组' : '暂无策略分组，点击「创建分组」按 view 应用 RPZ' }"
        :pagination="{ pageSize: 10, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono">view: {{ record.viewName }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'cidrs'">
            <div v-if="(record.cidrs ?? []).length > 0" class="flex flex-wrap gap-1">
              <span v-for="c in record.cidrs" :key="c" class="ipam-chip ipam-mono" data-tone="muted">{{ c }}</span>
            </div>
            <span v-else class="ipam-hint">全部来源</span>
          </template>
          <template v-else-if="column.dataIndex === 'listIds'">
            <span class="ipam-chip" data-tone="blue">{{ (record.listIds ?? []).length }} 个</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" @click="compilePg(record.id)">编译</Button>
          </template>
        </template>
      </Table>

      <div v-if="pgResult" class="mt-3">
        <div class="mb-2 flex items-center justify-between">
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:file-code-2" />
            编译结果
          </span>
          <Button size="small" @click="pgResult = null">收起</Button>
        </div>
        <pre class="ipam-code-block">{{ JSON.stringify(pgResult, null, 2) }}</pre>
      </div>
    </Card>

    <!-- 新建名单 -->
    <CreateModal class="w-[640px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">名单名称<em>*</em></span>
          <Input v-model:value="cForm.name" placeholder="如 恶意域名订阅" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">类型</span>
          <Select
            v-model:value="cForm.kind"
            :options="[
              { value: 'custom', label: '自定义（本地维护）' },
              { value: 'feed', label: '订阅源（HTTP 拉取）' },
            ]"
          />
        </label>
        <label v-if="cForm.kind === 'feed'" class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">订阅 URL<em>*</em></span>
          <Input v-model:value="cForm.syncUrl" placeholder="https://example.com/blocklist.txt" />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          订阅源每 30 秒检查一次更新，同步失败保留旧版本；创建后可在列表中添加条目。
        </div>
      </div>
    </CreateModal>

    <!-- 新建策略分组 -->
    <PgModal class="w-[680px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">分组名称<em>*</em></span>
          <Input v-model:value="pgForm.name" placeholder="如 访客网络" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">view 名称</span>
          <Input v-model:value="pgForm.viewName" placeholder="recursor" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">来源网段（CIDR）</span>
          <Input v-model:value="pgForm.cidrs" placeholder="逗号分隔，如 10.0.0.0/8,192.168.0.0/16" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">关联名单</span>
          <Select v-model:value="pgForm.listIds" mode="multiple" placeholder="选择生效的封禁名单" :options="pgListOptions" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">生效星期（留空=每天）</span>
          <Select v-model:value="pgForm.scheduleDays" mode="multiple" :options="WEEK_OPTIONS" placeholder="如仅工作日" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">开始时间</span>
          <Input v-model:value="pgForm.scheduleStart" placeholder="09:00" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">结束时间</span>
          <Input v-model:value="pgForm.scheduleEnd" placeholder="18:00" />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          view 名在同一 resolver 内唯一；白名单优先于黑名单生效。
          时段留空=全天生效；开始>结束表示跨零点（如 22:00→06:00），窗口外自动撤销该组拦截。
        </div>
      </div>
    </PgModal>

    <!-- 条目抽屉 -->
    <EntriesDrawer>
      <div class="mb-3 flex flex-wrap items-end gap-2">
        <div class="ipam-field">
          <span class="ipam-field__label">匹配规则</span>
          <Input v-model:value="eForm.pattern" placeholder="如 *.gamble.com 或 203.0.113.0/24" style="width: 200px" @press-enter="addEntry" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">触发</span>
          <Select
            v-model:value="eForm.triggerType"
            style="width: 150px"
            :options="[
              { value: 'qname', label: 'qname（域名）' },
              { value: 'response_ip', label: 'response_ip（应答IP）' },
            ]"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">动作</span>
          <Select
            v-model:value="eForm.action"
            style="width: 125px"
            :options="[
              { value: 'nxdomain', label: 'nxdomain' },
              { value: 'drop', label: 'drop' },
              { value: 'tcp_only', label: 'tcp_only' },
              { value: 'redirect', label: 'redirect' },
            ]"
          />
        </div>
        <Button type="primary" size="small" @click="addEntry">
          <IconifyIcon icon="lucide:plus" class="mr-1" />添加条目
        </Button>
        <Button size="small" @click="openCsv">
          <IconifyIcon icon="lucide:upload" class="mr-1" />导入 CSV
        </Button>
        <Button size="small" @click="exportCsv()">
          <IconifyIcon icon="lucide:download" class="mr-1" />导出 CSV
        </Button>
      </div>

      <div class="ipam-summary mb-3">
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">当前名单</span>
          <span class="ipam-summary__value" style="font-size: 14px">{{ selName }}</span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">条目总数</span>
          <span class="ipam-summary__value">{{ entries.length }}<small>条</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">域名匹配</span>
          <span class="ipam-summary__value">{{ entries.filter((e) => e.triggerType === 'qname').length }}<small>条</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">应答 IP</span>
          <span class="ipam-summary__value">{{ entries.filter((e) => e.triggerType === 'response_ip').length }}<small>条</small></span>
        </div>
      </div>

      <Table
        :data-source="entries"
        :loading="eLoading"
        :columns="[
          { title: '匹配规则', dataIndex: 'pattern', minWidth: 220 },
          { title: '触发', dataIndex: 'triggerType', width: 130 },
          { title: '动作', dataIndex: 'action', width: 120 },
          { title: '分类', dataIndex: 'category', width: 110 },
          { title: '操作', key: 'op', width: 90 },
        ]"
        :locale="{ emptyText: '该名单暂无条目，使用上方表单添加第一条规则' }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="pattern"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'pattern'">
            <span class="ipam-mono text-xs">{{ record.pattern }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'triggerType'">
            <Tag class="m-0" :color="record.triggerType === 'qname' ? 'blue' : 'cyan'">
              {{ record.triggerType }}
            </Tag>
          </template>
          <template v-else-if="column.dataIndex === 'action'">
            <span class="ipam-chip" :data-tone="record.action === 'drop' ? 'red' : record.action === 'nxdomain' ? 'amber' : 'muted'">
              {{ ACTION_TEXT[record.action] ?? record.action }}
            </span>
          </template>
          <template v-else-if="column.dataIndex === 'category'">
            <span class="ipam-hint">{{ record.category || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <Popconfirm title="确认删除该条目？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="removeEntry(record.listId, record.pattern)">
              <Button size="small" danger>删除</Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </EntriesDrawer>

    <!-- CSV 导入 -->
    <Modal
      v-model:open="csvOpen"
      title="CSV 导入名单条目"
      ok-text="开始导入"
      cancel-text="取消"
      :confirm-loading="csvImporting"
      @ok="submitCsv"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">CSV 内容（pattern[,action[,category]]）</span>
          <Input.TextArea
            v-model:value="csvContent"
            :rows="8"
            placeholder="pattern,action,category&#10;*.doubleclick.net,nxdomain,ads&#10;bad.example.com,redirect,gambling"
          />
        </div>
        <div class="ipam-form-field--wide flex items-center gap-2">
          <Switch v-model:checked="csvReplace" size="small" />
          <span class="ipam-hint">导入前清空该名单现有条目（会先保存版本快照，可回滚）</span>
        </div>
      </div>
    </Modal>

    <!-- 版本回滚 -->
    <Modal v-model:open="versionsOpen" title="版本快照与回滚" :footer="null" width="560px">
      <Table
        :data-source="versions"
        :loading="versionsLoading"
        :columns="[
          { title: '版本', dataIndex: 'version', width: 100 },
          { title: '条目数', dataIndex: 'entries', width: 100 },
          { title: '时间', dataIndex: 'createdAt', minWidth: 180 },
          { title: '操作', key: 'op', width: 110 },
        ]"
        :locale="{ emptyText: '暂无快照：订阅同步、CSV 导入前会自动保存' }"
        :pagination="false"
        row-key="version"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'version'">
            <span class="ipam-chip ipam-mono" data-tone="blue">v{{ record.version }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'createdAt'">
            <span class="ipam-hint">{{ fmtSync(record.createdAt) }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <Popconfirm
              title="确认回滚到该版本？"
              description="当前条目将被快照内容替换，版本号继续递增。"
              ok-text="回滚"
              cancel-text="取消"
              @confirm="doRollback(record.version)"
            >
              <Button size="small">回滚</Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </Modal>

    <!-- 内置分类库 -->
    <Modal v-model:open="builtinOpen" title="内置分类库" :footer="null" width="640px">
      <Table
        :data-source="packs"
        :loading="builtinLoading"
        :columns="[
          { title: '分类包', dataIndex: 'name', minWidth: 200 },
          { title: '条目', dataIndex: 'entryCount', width: 80 },
          { title: '操作', key: 'op', width: 100 },
        ]"
        :pagination="false"
        row-key="key"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta">{{ record.description }}</span>
            </div>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" type="primary" @click="installPack(record.key)">安装</Button>
          </template>
        </template>
      </Table>
      <div class="ipam-note mt-3">
        <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        <span>内置库为基线种子（非全量），安装后成为 builtin 名单，可继续增删条目或叠加订阅源。</span>
      </div>
    </Modal>
  </div>
</template>
