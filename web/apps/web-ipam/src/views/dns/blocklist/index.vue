<script setup lang="ts">
import type { Blocklist, BlocklistEntryRow, PolicyGroupRow } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { useVbenDrawer, useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

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
} from 'ant-design-vue';

import {
  addBlocklistEntry,
  compilePolicyGroup,
  createBlocklist,
  createPolicyGroup,
  deleteBlocklist,
  deleteBlocklistEntry,
  exportBlocklistCsv,
  importBlocklistEntries,
  installBuiltinPack,
  listBlocklistEntries,
  listBlocklists,
  listBlocklistVersions,
  listBuiltinPacks,
  listPolicyGroups,
  rollbackBlocklist,
  syncBlocklist,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const showTotal = (t: number) => $t('dns.common.total', { n: t });

const rows = ref<Blocklist[]>([]);
const loading = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

const entries = ref<BlocklistEntryRow[]>([]);
const eLoading = ref(false);
const selId = ref('');
const selName = ref('');
const eForm = ref<{
  action: 'drop' | 'nxdomain' | 'redirect' | 'tcp_only';
  pattern: string;
  triggerType: 'qname' | 'response_ip';
}>({ pattern: '', triggerType: 'qname', action: 'nxdomain' });
const cForm = ref<{
  kind: 'builtin' | 'custom' | 'feed';
  name: string;
  syncUrl: string;
}>({ name: '', kind: 'custom', syncUrl: '' });
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
}>({
  name: '',
  viewName: 'recursor',
  cidrs: '',
  listIds: [],
  scheduleDays: [],
  scheduleStart: '',
  scheduleEnd: '',
});
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
    message.success($t('dns.blocklist.msg.syncSubmitted'));
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.syncFailed'),
    );
  } finally {
    syncingId.value = '';
  }
}
async function removeList(id: string) {
  try {
    await deleteBlocklist(id);
    message.success($t('dns.blocklist.msg.listDeleted'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.deleteFailed'),
    );
  }
  await load();
}

const [CreateModal, createModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.blocklist.newList'),
  confirmText: $t('dns.blocklist.createConfirm'),
  onConfirm: () => createList(),
});
function openCreate() {
  cForm.value = { name: '', kind: 'custom', syncUrl: '' };
  createModalApi.open();
}
async function createList() {
  if (!cForm.value.name) {
    message.warning($t('dns.blocklist.msg.nameRequired'));
    return;
  }
  if (cForm.value.kind === 'feed' && !cForm.value.syncUrl) {
    message.warning($t('dns.blocklist.msg.feedUrlRequired'));
    return;
  }
  try {
    await createBlocklist({
      name: cForm.value.name,
      kind: cForm.value.kind,
      syncUrl: cForm.value.syncUrl || undefined,
    });
    message.success($t('dns.blocklist.msg.listCreated'));
    cForm.value = { name: '', kind: 'custom', syncUrl: '' };
    createModalApi.close();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.createFailed'),
    );
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
const [PgModal, pgModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.blocklist.pgModal.title'),
  confirmText: $t('dns.blocklist.pgModal.createConfirm'),
  onConfirm: () => createPg(),
});
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
    message.warning($t('dns.blocklist.msg.pgNameRequired'));
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
      cidrs: pgForm.value.cidrs
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      listIds: pgForm.value.listIds,
      schedule,
    });
    message.success($t('dns.blocklist.msg.pgCreated'));
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
    message.error(
      error instanceof Error ? error.message : $t('dns.common.createFailed'),
    );
  }
  await loadPolicyGroups();
}
async function compilePg(id: string) {
  try {
    pgResult.value = await compilePolicyGroup(id);
    message.success($t('dns.blocklist.msg.compileOk'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.compileFailed'),
    );
  }
}

// ── 条目抽屉（M2-046：条目在抽屉中展示 + 客户端分页）──
const [EntriesDrawer, entriesDrawerApi] = useVbenDrawer({
  class: 'w-[780px]',
  footer: false,
  title: $t('dns.blocklist.entries.title'),
});
async function openEntries(r: Blocklist) {
  selId.value = r.id;
  selName.value = r.name;
  entries.value = [];
  entriesDrawerApi.setState({
    title: $t('dns.blocklist.entries.titleNamed', { name: r.name }),
  });
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
    message.warning($t('dns.blocklist.msg.patternRequired'));
    return;
  }
  try {
    await addBlocklistEntry(selId.value, {
      pattern: eForm.value.pattern,
      triggerType: eForm.value.triggerType,
      action: eForm.value.action,
    });
    message.success($t('dns.blocklist.msg.entryAdded'));
    eForm.value = { pattern: '', triggerType: 'qname', action: 'nxdomain' };
    await loadEntries();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.addFailed'),
    );
  }
}
async function removeEntry(listId: string, pattern: string) {
  try {
    await deleteBlocklistEntry(listId, pattern);
    message.success($t('dns.blocklist.msg.entryDeleted'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.deleteFailed'),
    );
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
    message.warning($t('dns.blocklist.msg.csvNeedContent'));
    return;
  }
  csvImporting.value = true;
  try {
    const r = await importBlocklistEntries(
      selId.value,
      csvContent.value,
      csvReplace.value,
    );
    message.success(
      $t('dns.blocklist.msg.csvImportOk', {
        created: r.created,
        skipped: r.skipped,
        total: r.total,
      }),
    );
    if (r.errors?.length)
      message.warning(r.errors.slice(0, 3).join($t('dns.common.listSep')));
    csvOpen.value = false;
    await Promise.all([loadEntries(), load()]);
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.csvImportFailed'),
    );
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
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.csvExportFailed'),
    );
  }
}

const versionsOpen = ref(false);
const versionsLoading = ref(false);
const versions = ref<{ createdAt: string; entries: number; version: number }[]>(
  [],
);
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
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.versionsLoadFailed'),
    );
  } finally {
    versionsLoading.value = false;
  }
}
async function doRollback(version: number) {
  try {
    await rollbackBlocklist(selId.value, version);
    message.success($t('dns.blocklist.msg.rollbackOk', { version }));
    versionsOpen.value = false;
    await Promise.all([load(), loadEntries()]);
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.rollbackFailed'),
    );
  }
}

const builtinOpen = ref(false);
const builtinLoading = ref(false);
const packs = ref<
  {
    categories?: string[];
    description?: string;
    entryCount: number;
    key: string;
    name: string;
  }[]
>([]);
async function openBuiltin() {
  builtinLoading.value = true;
  builtinOpen.value = true;
  try {
    const data = await listBuiltinPacks();
    packs.value = data.items ?? [];
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.builtinLoadFailed'),
    );
  } finally {
    builtinLoading.value = false;
  }
}
async function installPack(key: string) {
  try {
    const b = await installBuiltinPack(key);
    message.success(
      $t('dns.blocklist.msg.installed', {
        name: b.name,
        version: b.version ? `v${b.version}` : '',
      }),
    );
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.msg.installFailed'),
    );
  }
}

const WEEK_OPTIONS = computed(() => [
  { label: $t('dns.blocklist.week.sun'), value: 0 },
  { label: $t('dns.blocklist.week.mon'), value: 1 },
  { label: $t('dns.blocklist.week.tue'), value: 2 },
  { label: $t('dns.blocklist.week.wed'), value: 3 },
  { label: $t('dns.blocklist.week.thu'), value: 4 },
  { label: $t('dns.blocklist.week.fri'), value: 5 },
  { label: $t('dns.blocklist.week.sat'), value: 6 },
]);

const KIND_TEXT = computed<Record<string, string>>(() => ({
  builtin: $t('dns.blocklist.kind.builtin'),
  custom: $t('dns.blocklist.kind.custom'),
  feed: $t('dns.blocklist.kind.feed'),
}));
const KIND_COLOR: Record<string, string> = {
  builtin: 'default',
  custom: 'blue',
  feed: 'purple',
};
const ACTION_TEXT = computed<Record<string, string>>(() => ({
  drop: $t('dns.blocklist.action.drop'),
  nxdomain: $t('dns.blocklist.action.nxdomain'),
  redirect: $t('dns.blocklist.action.redirect'),
  tcp_only: $t('dns.blocklist.action.tcpOnly'),
}));
function fmtSync(v?: string) {
  if (!v) return '—';
  const date = new Date(v);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1970) return '—';
  return date.toLocaleString(undefined, { hour12: false });
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
const kindOptions = computed(() => [
  { label: $t('dns.common.allTypes'), value: 'all' },
  { label: $t('dns.blocklist.kindOptions.builtin'), value: 'builtin' },
  { label: $t('dns.blocklist.kindOptions.custom'), value: 'custom' },
  { label: $t('dns.blocklist.kindOptions.feed'), value: 'feed' },
]);
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
    `${r.name} ${r.viewName} ${(r.cidrs ?? []).join(' ')}`
      .toLowerCase()
      .includes(kw),
  );
});
const pgListOptions = computed(() =>
  rows.value.map((r) => ({
    label: `${r.name}（${KIND_TEXT.value[r.kind] ?? r.kind}）`,
    value: r.id,
  })),
);

const listColumns = computed(() => [
  { title: $t('dns.blocklist.list.colName'), dataIndex: 'name', minWidth: 280 },
  { title: $t('dns.common.type'), dataIndex: 'kind', width: 100 },
  {
    title: $t('dns.blocklist.list.colSync'),
    dataIndex: 'lastSync',
    minWidth: 200,
  },
  { title: $t('dns.common.actions'), key: 'op', width: 240 },
]);
const pgColumns = computed(() => [
  { title: $t('dns.blocklist.pg.colName'), dataIndex: 'name', minWidth: 220 },
  { title: $t('dns.blocklist.pg.colCidrs'), dataIndex: 'cidrs', minWidth: 240 },
  { title: $t('dns.blocklist.pg.colLists'), dataIndex: 'listIds', width: 100 },
  { title: $t('dns.common.actions'), key: 'op', width: 120 },
]);
const entryColumns = computed(() => [
  {
    title: $t('dns.blocklist.entries.colPattern'),
    dataIndex: 'pattern',
    minWidth: 220,
  },
  {
    title: $t('dns.blocklist.entries.colTrigger'),
    dataIndex: 'triggerType',
    width: 130,
  },
  {
    title: $t('dns.blocklist.entries.colAction'),
    dataIndex: 'action',
    width: 120,
  },
  {
    title: $t('dns.blocklist.entries.colCategory'),
    dataIndex: 'category',
    width: 110,
  },
  { title: $t('dns.common.actions'), key: 'op', width: 90 },
]);
const versionColumns = computed(() => [
  {
    title: $t('dns.blocklist.versions.colVersion'),
    dataIndex: 'version',
    width: 100,
  },
  {
    title: $t('dns.blocklist.versions.colEntries'),
    dataIndex: 'entries',
    width: 100,
  },
  {
    title: $t('dns.blocklist.versions.colTime'),
    dataIndex: 'createdAt',
    minWidth: 180,
  },
  { title: $t('dns.common.actions'), key: 'op', width: 110 },
]);
const packColumns = computed(() => [
  {
    title: $t('dns.blocklist.builtin.colPack'),
    dataIndex: 'name',
    minWidth: 200,
  },
  {
    title: $t('dns.blocklist.builtin.colEntries'),
    dataIndex: 'entryCount',
    width: 80,
  },
  { title: $t('dns.common.actions'), key: 'op', width: 100 },
]);

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
      :title="$t('dns.blocklist.header.title')"
      :description="$t('dns.blocklist.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:list-filter" class="size-3.5" />
          {{
            $t('dns.blocklist.header.meta', {
              lists: stats.total,
              groups: stats.groups,
            })
          }}
        </span>
        <Button
          size="small"
          :loading="loading"
          @click="
            load();
            loadPolicyGroups();
          "
        >
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dns.common.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.blocklist.newList')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dns.blocklist.stat.lists')"
        :value="stats.total"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:list-filter"
        tone="blue"
        :hint="
          $t('dns.blocklist.stat.listsHint', {
            custom: stats.custom,
            feeds: stats.feeds,
          })
        "
      />
      <StatCard
        :label="$t('dns.blocklist.stat.feeds')"
        :value="stats.feeds"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:rss"
        tone="violet"
        :hint="
          stats.feeds > 0
            ? $t('dns.blocklist.stat.feedsHintOn')
            : $t('dns.blocklist.stat.feedsHintOff')
        "
      />
      <StatCard
        :label="$t('dns.blocklist.stat.groups')"
        :value="stats.groups"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:network"
        tone="cyan"
        :hint="
          stats.groups > 0
            ? $t('dns.blocklist.stat.groupsHintOn')
            : $t('dns.blocklist.stat.groupsHintOff')
        "
      />
      <StatCard
        :label="$t('dns.blocklist.stat.builtin')"
        :value="stats.builtin"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:lock"
        tone="green"
        :hint="
          stats.builtin > 0
            ? $t('dns.blocklist.stat.builtinHintOn')
            : $t('dns.blocklist.stat.builtinHintOff')
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-ban" />
          {{ $t('dns.blocklist.listCard.title') }}
          <em class="ipam-card-sub">{{
            $t('dns.blocklist.listCard.sub', {
              filtered: filteredLists.length,
              total: rows.length,
            })
          }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.blocklist.listCard.hint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.common.keyword') }}</span>
          <Input
            v-model:value="listKeyword"
            allow-clear
            :placeholder="$t('dns.blocklist.list.kwPlaceholder')"
            style="width: 240px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.common.type') }}</span>
          <Select
            v-model:value="listKind"
            :options="kindOptions"
            style="width: 140px"
          />
        </div>
        <Button
          size="small"
          @click="
            listKeyword = '';
            listKind = 'all';
          "
        >
          {{ $t('dns.common.reset') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="openBuiltin">
          <IconifyIcon icon="lucide:package" class="mr-1" />{{
            $t('dns.blocklist.list.builtinLib')
          }}
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.blocklist.newList')
          }}
        </Button>
      </div>

      <Table
        :data-source="filteredLists"
        :loading="loading"
        :columns="listColumns"
        :locale="{
          emptyText:
            listKeyword || listKind !== 'all'
              ? $t('dns.blocklist.list.emptyFiltered')
              : $t('dns.blocklist.list.empty'),
        }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono">{{
                record.syncUrl ||
                (record.kind === 'builtin'
                  ? $t('dns.blocklist.list.metaBuiltin')
                  : $t('dns.blocklist.list.metaLocal'))
              }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'kind'">
            <Tag class="m-0" :color="KIND_COLOR[record.kind] ?? 'default'">
              {{ KIND_TEXT[record.kind] ?? record.kind }}
            </Tag>
          </template>
          <template v-else-if="column.dataIndex === 'lastSync'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title" style="font-weight: 400">{{
                fmtSync(record.lastSync)
              }}</span>
              <span class="ipam-cell-meta">{{
                $t('dns.blocklist.list.version', { v: record.version })
              }}</span>
            </div>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="openEntries(record as Blocklist)">
                {{ $t('dns.blocklist.list.btnEntries') }}
              </Button>
              <Button size="small" @click="exportCsv(record.id)">
                {{ $t('dns.blocklist.list.btnExport') }}
              </Button>
              <Button size="small" @click="openVersions(record as Blocklist)">
                {{ $t('dns.blocklist.list.btnVersions') }}
              </Button>
              <Button
                v-if="record.kind === 'feed'"
                size="small"
                :loading="syncingId === record.id"
                @click="sync(record.id)"
              >
                {{ $t('dns.blocklist.list.btnSync') }}
              </Button>
              <Popconfirm
                v-if="record.kind !== 'builtin'"
                :title="$t('dns.blocklist.list.deleteTitle')"
                :description="$t('dns.blocklist.list.deleteDesc')"
                :ok-text="$t('dns.common.delete')"
                :cancel-text="$t('dns.common.cancel')"
                @confirm="removeList(record.id)"
              >
                <Button size="small" danger>
                  {{ $t('dns.common.delete') }}
                </Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>

      <div class="ipam-note mt-3">
        <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        <span>{{ $t('dns.blocklist.note') }}</span>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:network" />
          {{ $t('dns.blocklist.pg.title') }}
          <em class="ipam-card-sub">{{
            $t('dns.blocklist.pg.sub', {
              filtered: filteredGroups.length,
              total: pgRows.length,
            })
          }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.blocklist.pg.hint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.common.keyword') }}</span>
          <Input
            v-model:value="pgKeyword"
            allow-clear
            :placeholder="$t('dns.blocklist.pg.kwPlaceholder')"
            style="width: 240px"
          />
        </div>
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="openCreatePg">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.blocklist.pg.createBtn')
          }}
        </Button>
      </div>

      <Table
        :data-source="filteredGroups"
        :loading="pgLoading"
        :columns="pgColumns"
        :locale="{
          emptyText: pgKeyword
            ? $t('dns.blocklist.pg.emptyFiltered')
            : $t('dns.blocklist.pg.empty'),
        }"
        :pagination="{ pageSize: 10, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono"
                >view: {{ record.viewName }}</span
              >
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'cidrs'">
            <div
              v-if="(record.cidrs ?? []).length > 0"
              class="flex flex-wrap gap-1"
            >
              <span
                v-for="c in record.cidrs"
                :key="c"
                class="ipam-chip ipam-mono"
                data-tone="muted"
                >{{ c }}</span
              >
            </div>
            <span v-else class="ipam-hint">{{
              $t('dns.blocklist.pg.allSources')
            }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'listIds'">
            <span class="ipam-chip" data-tone="blue">{{
              $t('dns.common.count', { n: (record.listIds ?? []).length })
            }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" @click="compilePg(record.id)">
              {{ $t('dns.blocklist.pg.compile') }}
            </Button>
          </template>
        </template>
      </Table>

      <div v-if="pgResult" class="mt-3">
        <div class="mb-2 flex items-center justify-between">
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:file-code-2" />
            {{ $t('dns.blocklist.pg.compileResult') }}
          </span>
          <Button size="small" @click="pgResult = null">
            {{ $t('dns.blocklist.pg.collapse') }}
          </Button>
        </div>
        <pre class="ipam-code-block">{{
          JSON.stringify(pgResult, null, 2)
        }}</pre>
      </div>
    </Card>

    <!-- 新建名单 -->
    <CreateModal class="w-[640px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dns.blocklist.createModal.nameLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="cForm.name"
            :placeholder="$t('dns.blocklist.createModal.namePlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.common.type')
          }}</span>
          <Select
            v-model:value="cForm.kind"
            :options="[
              {
                value: 'custom',
                label: $t('dns.blocklist.createModal.optCustom'),
              },
              { value: 'feed', label: $t('dns.blocklist.createModal.optFeed') },
            ]"
          />
        </label>
        <label
          v-if="cForm.kind === 'feed'"
          class="ipam-form-field ipam-form-field--wide"
        >
          <span class="ipam-form-field__label"
            >{{ $t('dns.blocklist.createModal.urlLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="cForm.syncUrl"
            placeholder="https://example.com/blocklist.txt"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.blocklist.createModal.hint') }}
        </div>
      </div>
    </CreateModal>

    <!-- 新建策略分组 -->
    <PgModal class="w-[680px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dns.blocklist.pgModal.nameLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="pgForm.name"
            :placeholder="$t('dns.blocklist.pgModal.namePlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.pgModal.viewLabel')
          }}</span>
          <Input v-model:value="pgForm.viewName" placeholder="recursor" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.pgModal.cidrLabel')
          }}</span>
          <Input
            v-model:value="pgForm.cidrs"
            :placeholder="$t('dns.blocklist.pgModal.cidrPlaceholder')"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.pgModal.listsLabel')
          }}</span>
          <Select
            v-model:value="pgForm.listIds"
            mode="multiple"
            :placeholder="$t('dns.blocklist.pgModal.listsPlaceholder')"
            :options="pgListOptions"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.pgModal.weekLabel')
          }}</span>
          <Select
            v-model:value="pgForm.scheduleDays"
            mode="multiple"
            :options="WEEK_OPTIONS"
            :placeholder="$t('dns.blocklist.pgModal.weekPlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.pgModal.startLabel')
          }}</span>
          <Input
            v-model:value="pgForm.scheduleStart"
            :placeholder="$t('dns.blocklist.pgModal.startPlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.pgModal.endLabel')
          }}</span>
          <Input
            v-model:value="pgForm.scheduleEnd"
            :placeholder="$t('dns.blocklist.pgModal.endPlaceholder')"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.blocklist.pgModal.hint') }}
        </div>
      </div>
    </PgModal>

    <!-- 条目抽屉 -->
    <EntriesDrawer>
      <div class="mb-3 flex flex-wrap items-end gap-2">
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.blocklist.entries.patternLabel')
          }}</span>
          <Input
            v-model:value="eForm.pattern"
            :placeholder="$t('dns.blocklist.entries.patternPlaceholder')"
            style="width: 200px"
            @press-enter="addEntry"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.blocklist.entries.triggerLabel')
          }}</span>
          <Select
            v-model:value="eForm.triggerType"
            style="width: 150px"
            :options="[
              {
                value: 'qname',
                label: $t('dns.blocklist.entries.triggerQname'),
              },
              {
                value: 'response_ip',
                label: $t('dns.blocklist.entries.triggerIp'),
              },
            ]"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.blocklist.entries.actionLabel')
          }}</span>
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
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.blocklist.entries.addBtn')
          }}
        </Button>
        <Button size="small" @click="openCsv">
          <IconifyIcon icon="lucide:upload" class="mr-1" />{{
            $t('dns.blocklist.entries.importCsv')
          }}
        </Button>
        <Button size="small" @click="exportCsv()">
          <IconifyIcon icon="lucide:download" class="mr-1" />{{
            $t('dns.blocklist.entries.exportCsv')
          }}
        </Button>
      </div>

      <div class="ipam-summary mb-3">
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.blocklist.entries.currentList')
          }}</span>
          <span class="ipam-summary__value" style="font-size: 14px">{{
            selName
          }}</span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.blocklist.entries.totalEntries')
          }}</span>
          <span class="ipam-summary__value"
            >{{ entries.length
            }}<small>{{ $t('dns.common.unit.item') }}</small></span
          >
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.blocklist.entries.qnameEntries')
          }}</span>
          <span class="ipam-summary__value"
            >{{ entries.filter((e) => e.triggerType === 'qname').length
            }}<small>{{ $t('dns.common.unit.item') }}</small></span
          >
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.blocklist.entries.ipEntries')
          }}</span>
          <span class="ipam-summary__value"
            >{{ entries.filter((e) => e.triggerType === 'response_ip').length
            }}<small>{{ $t('dns.common.unit.item') }}</small></span
          >
        </div>
      </div>

      <Table
        :data-source="entries"
        :loading="eLoading"
        :columns="entryColumns"
        :locale="{ emptyText: $t('dns.blocklist.entries.empty') }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="pattern"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'pattern'">
            <span class="ipam-mono text-xs">{{ record.pattern }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'triggerType'">
            <Tag
              class="m-0"
              :color="record.triggerType === 'qname' ? 'blue' : 'cyan'"
            >
              {{ record.triggerType }}
            </Tag>
          </template>
          <template v-else-if="column.dataIndex === 'action'">
            <span
              class="ipam-chip"
              :data-tone="
                record.action === 'drop'
                  ? 'red'
                  : record.action === 'nxdomain'
                    ? 'amber'
                    : 'muted'
              "
            >
              {{ ACTION_TEXT[record.action] ?? record.action }}
            </span>
          </template>
          <template v-else-if="column.dataIndex === 'category'">
            <span class="ipam-hint">{{ record.category || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <Popconfirm
              :title="$t('dns.blocklist.entries.deleteTitle')"
              :description="$t('dns.blocklist.entries.deleteDesc')"
              :ok-text="$t('dns.common.delete')"
              :cancel-text="$t('dns.common.cancel')"
              @confirm="removeEntry(record.listId, record.pattern)"
            >
              <Button size="small" danger>{{ $t('dns.common.delete') }}</Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </EntriesDrawer>

    <!-- CSV 导入 -->
    <Modal
      v-model:open="csvOpen"
      :title="$t('dns.blocklist.csv.title')"
      :ok-text="$t('dns.blocklist.csv.ok')"
      :cancel-text="$t('dns.common.cancel')"
      :confirm-loading="csvImporting"
      @ok="submitCsv"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.blocklist.csv.contentLabel')
          }}</span>
          <Input.TextArea
            v-model:value="csvContent"
            :rows="8"
            placeholder="pattern,action,category&#10;*.doubleclick.net,nxdomain,ads&#10;bad.example.com,redirect,gambling"
          />
        </div>
        <div class="ipam-form-field--wide flex items-center gap-2">
          <Switch v-model:checked="csvReplace" size="small" />
          <span class="ipam-hint">{{ $t('dns.blocklist.csv.hint') }}</span>
        </div>
      </div>
    </Modal>

    <!-- 版本回滚 -->
    <Modal
      v-model:open="versionsOpen"
      :title="$t('dns.blocklist.versions.title')"
      :footer="null"
      width="560px"
    >
      <Table
        :data-source="versions"
        :loading="versionsLoading"
        :columns="versionColumns"
        :locale="{ emptyText: $t('dns.blocklist.versions.empty') }"
        :pagination="false"
        row-key="version"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'version'">
            <span class="ipam-chip ipam-mono" data-tone="blue"
              >v{{ record.version }}</span
            >
          </template>
          <template v-else-if="column.dataIndex === 'createdAt'">
            <span class="ipam-hint">{{ fmtSync(record.createdAt) }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <Popconfirm
              :title="$t('dns.blocklist.versions.rollbackTitle')"
              :description="$t('dns.blocklist.versions.rollbackDesc')"
              :ok-text="$t('dns.blocklist.versions.rollbackBtn')"
              :cancel-text="$t('dns.common.cancel')"
              @confirm="doRollback(record.version)"
            >
              <Button size="small">
                {{ $t('dns.blocklist.versions.rollbackBtn') }}
              </Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </Modal>

    <!-- 内置分类库 -->
    <Modal
      v-model:open="builtinOpen"
      :title="$t('dns.blocklist.list.builtinLib')"
      :footer="null"
      width="640px"
    >
      <Table
        :data-source="packs"
        :loading="builtinLoading"
        :columns="packColumns"
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
            <Button
              size="small"
              type="primary"
              @click="installPack(record.key)"
            >
              {{ $t('dns.blocklist.builtin.installBtn') }}
            </Button>
          </template>
        </template>
      </Table>
      <div class="ipam-note mt-3">
        <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        <span>{{ $t('dns.blocklist.builtin.note') }}</span>
      </div>
    </Modal>
  </div>
</template>
