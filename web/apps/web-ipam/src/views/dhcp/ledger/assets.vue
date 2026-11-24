<script setup lang="ts">
import type { Asset, AssetStats, OrgTreeNode } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  message,
  Popconfirm,
  Select,
  Table,
  Tag,
} from 'ant-design-vue';

import { deleteAsset, listAssets, listOrgTree, upsertAsset } from '#/api/ipam';
import OrgFilterCard from '#/components/org-filter-card.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';
import { normalizeMacInput } from '#/utils/mac';

const showTotal = (t: number) =>
  $t('dhcpLedger.common.showTotal', { total: t });

// ── 组织筛选（左侧树） ──
const orgTree = ref<OrgTreeNode[]>([]);
const selectedOrgId = ref('');

function onSelectOrg(orgId: string) {
  selectedOrgId.value = orgId;
  void load();
}

/** 组织 id → 名称 / 完整路径（台账关联链展示用） */
const orgPathMap = computed(() => {
  const m = new Map<string, string[]>();
  const walk = (nodes: OrgTreeNode[], trail: string[]) => {
    for (const n of nodes) {
      const next = [...trail, n.name];
      m.set(n.id, next);
      if (n.children?.length) walk(n.children, next);
    }
  };
  walk(orgTree.value, []);
  return m;
});

function orgPathOf(orgId?: string): string[] {
  if (!orgId) return [];
  return orgPathMap.value.get(orgId) ?? [];
}

/** 组织树 → 表单下拉（缩进表示层级） */
const orgOptions = computed(() => {
  const out: { label: string; value: string }[] = [];
  const walk = (nodes: OrgTreeNode[], depth: number) => {
    for (const n of nodes) {
      out.push({ label: `${'　'.repeat(depth)}${n.name}`, value: n.id });
      if (n.children?.length) walk(n.children, depth + 1);
    }
  };
  walk(orgTree.value, 0);
  return out;
});

// ── 资产清单（服务端分页 + 服务端统计，M6-048）──
const rows = ref<Asset[]>([]);
const loading = ref(false);
const keyword = ref('');
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
const stats = ref<AssetStats>({
  assigned: 0,
  depts: 0,
  offline: 0,
  online: 0,
  owners: 0,
  tagged: 0,
  total: 0,
  unassigned: 0,
});
const pagination = computed(() => ({
  current: page.value,
  pageSize: pageSize.value,
  showSizeChanger: true,
  showTotal,
  total: total.value,
}));

/** 表头分页变化：切页/改每页条数都重新请求，避免前端"假分页"。 */
function onTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) page.value = p.current;
  if (p.pageSize) pageSize.value = p.pageSize;
  void load();
}
type Scope = 'all' | 'tagged' | 'unassigned';
const scope = ref<Scope>('all');
const scopeOptions = computed(() => [
  { label: $t('dhcpLedger.assets.scopeAll'), value: 'all' },
  { label: $t('dhcpLedger.assets.scopeUnassigned'), value: 'unassigned' },
  { label: $t('dhcpLedger.assets.scopeTagged'), value: 'tagged' },
]);
// 在线状态走服务端过滤（租约视图由后端 join 联动绑定，M6-047）。
type OnlineFilter = 'all' | 'offline' | 'online';
const onlineFilter = ref<OnlineFilter>('all');
const onlineOptions = computed(() => [
  { label: $t('dhcpLedger.assets.onlineAll'), value: 'all' },
  { label: $t('dhcpLedger.assets.onlineOnly'), value: 'online' },
  { label: $t('dhcpLedger.assets.offlineOnly'), value: 'offline' },
]);

async function load() {
  loading.value = true;
  try {
    const res = await listAssets({
      orgId: selectedOrgId.value || undefined,
      q: keyword.value.trim() || undefined,
      online:
        onlineFilter.value === 'all'
          ? undefined
          : onlineFilter.value === 'online',
      page: page.value,
      pageSize: pageSize.value,
      scope: scope.value,
    });
    rows.value = res.items ?? [];
    total.value = res.total ?? 0;
    if (res.stats) stats.value = res.stats;
  } finally {
    loading.value = false;
  }
}

/** 改筛选条件一律回到第 1 页，否则会落在空页。 */
function applyFilters() {
  page.value = 1;
  void load();
}

function resetFilters() {
  keyword.value = '';
  scope.value = 'all';
  onlineFilter.value = 'all';
  applyFilters();
}

/** 组织树选择在父组件回调里触发，需同步重置页码。 */
function onSelectOrgAndReload(orgId: string) {
  page.value = 1;
  onSelectOrg(orgId);
}

const assetColumns = computed(() => [
  {
    title: $t('dhcpLedger.assets.colTerminal'),
    dataIndex: 'mac',
    minWidth: 190,
  },
  {
    title: $t('dhcpLedger.assets.colAddress'),
    dataIndex: 'ipv4',
    minWidth: 170,
  },
  {
    title: $t('dhcpLedger.assets.colOwnerDept'),
    dataIndex: 'owner',
    minWidth: 130,
  },
  { title: $t('dhcpLedger.assets.colOrg'), dataIndex: 'orgId', minWidth: 180 },
  { title: $t('dhcpLedger.assets.colTags'), dataIndex: 'tags', width: 130 },
  { title: $t('dhcpLedger.assets.colOnline'), dataIndex: 'online', width: 110 },
  { title: $t('dhcpLedger.common.actions'), key: 'op', width: 110 },
]);

const emptyText = computed(() =>
  keyword.value || scope.value !== 'all'
    ? $t('dhcpLedger.assets.emptyFiltered')
    : $t('dhcpLedger.assets.emptyList'),
);

// ── CSV 导出（当前筛选结果，含完整组织路径） ──
function csvCell(v: unknown): string {
  const s = String(v ?? '');
  const needsQuote = s.includes('"') || s.includes(',') || s.includes('\n');
  return needsQuote ? `"${s.replaceAll('"', '""')}"` : s;
}

async function exportCsv() {
  // 服务端分页后前端只有当前页：导出时按同一筛选最多取 500 条（与批量登记上限一致）。
  const res = await listAssets({
    orgId: selectedOrgId.value || undefined,
    q: keyword.value.trim() || undefined,
    online:
      onlineFilter.value === 'all'
        ? undefined
        : onlineFilter.value === 'online',
    page: 1,
    pageSize: 500,
    scope: scope.value,
  });
  const exportRows = res.items ?? [];
  const header = [
    $t('dhcpLedger.assets.csv.mac'),
    $t('dhcpLedger.assets.csv.duid'),
    $t('dhcpLedger.assets.csv.ipv4'),
    $t('dhcpLedger.assets.csv.ipv6'),
    $t('dhcpLedger.assets.csv.online'),
    $t('dhcpLedger.assets.csv.owner'),
    $t('dhcpLedger.assets.csv.dept'),
    $t('dhcpLedger.assets.csv.org'),
    $t('dhcpLedger.assets.csv.tags'),
    $t('dhcpLedger.assets.csv.note'),
  ];
  const lines = exportRows.map((r) =>
    [
      r.mac ?? '',
      r.duid ?? '',
      r.ipv4 ?? '',
      r.ipv6 ?? '',
      r.online ? '1' : '0',
      r.owner,
      r.dept ?? '',
      orgPathOf(r.orgId).join(' / '),
      (r.tags ?? []).join('|'),
      r.note ?? '',
    ]
      .map((v) => csvCell(v))
      .join(','),
  );
  const csv = `\uFEFF${[header.join(','), ...lines].join('\r\n')}`;
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `assets-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  URL.revokeObjectURL(a.href);
  message.success(
    $t('dhcpLedger.assets.exportDone', { count: exportRows.length }),
  );
}

// ── 登记 / 编辑 ──
const editingIdentity = ref('');
const form = reactive({
  mac: '',
  duid: '',
  owner: '',
  dept: '',
  orgId: undefined as string | undefined,
  tags: [] as string[],
  note: '',
});

const [FormModal, formModalApi] = useVbenModal({
  draggable: true,
  title: $t('dhcpLedger.assets.modalCreate'),
  confirmText: $t('dhcpLedger.assets.modalSave'),
  onConfirm: () => save(),
});

function resetForm() {
  editingIdentity.value = '';
  form.mac = '';
  form.duid = '';
  form.owner = '';
  form.dept = '';
  form.orgId = undefined;
  form.tags = [];
  form.note = '';
}

function openCreate() {
  resetForm();
  formModalApi.setState({
    title: $t('dhcpLedger.assets.modalCreate'),
    confirmText: $t('dhcpLedger.assets.modalRegister'),
  });
  formModalApi.open();
}

function openEdit(r: Asset) {
  resetForm();
  editingIdentity.value = r.identity;
  form.mac = r.mac ?? '';
  form.duid = r.duid ?? '';
  form.owner = r.owner ?? '';
  form.dept = r.dept ?? '';
  form.orgId = r.orgId || undefined;
  form.tags = [...(r.tags ?? [])];
  form.note = r.note ?? '';
  formModalApi.setState({
    title: $t('dhcpLedger.assets.modalEditTitle', {
      mac: r.mac || $t('dhcpLedger.assets.duidOnly'),
    }),
    confirmText: $t('dhcpLedger.assets.modalSaveChanges'),
  });
  formModalApi.open();
}

async function save() {
  const rawMac = form.mac.trim();
  const mac = rawMac ? normalizeMacInput(rawMac) : null;
  if (rawMac && !mac) {
    message.warning($t('dhcpLedger.common.invalidMac'));
    return;
  }
  // DUID 归一化：小写、去分隔符（IPv6-only 终端的身份键，M6-046）。
  const duid = form.duid
    .trim()
    .toLowerCase()
    .replaceAll(/[:\-.\s]/g, '');
  if (duid && !/^[0-9a-f]+$/.test(duid)) {
    message.warning($t('dhcpLedger.assets.invalidDuid'));
    return;
  }
  if (!mac && !duid) {
    message.warning($t('dhcpLedger.assets.identityRequired'));
    return;
  }
  if (!form.owner.trim()) {
    message.warning($t('dhcpLedger.assets.ownerRequired'));
    return;
  }
  try {
    await upsertAsset({
      mac: mac ?? undefined,
      duid: duid || undefined,
      owner: form.owner.trim(),
      dept: form.dept.trim() || undefined,
      orgId: form.orgId || undefined,
      // 始终传数组：后端 tags 列为 NOT NULL，显式缺省会被编码为 NULL（详见 M2-048 修复）
      tags: [...form.tags],
      note: form.note.trim() || undefined,
    });
    message.success(
      editingIdentity.value
        ? $t('dhcpLedger.assets.updated')
        : $t('dhcpLedger.assets.created'),
    );
    formModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.assets.saveFailed'),
    );
  }
}

async function remove(identity: string) {
  try {
    await deleteAsset(identity);
    message.success($t('dhcpLedger.assets.deleted'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.assets.deleteFailed'),
    );
  }
  await load();
}

onMounted(async () => {
  orgTree.value = await listOrgTree();
  await load();
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:contact"
      :title="$t('dhcpLedger.assets.title')"
      :description="$t('dhcpLedger.assets.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:contact-round" class="size-3.5" />
          {{
            $t('dhcpLedger.assets.meta', {
              total: stats.total,
              departments: stats.depts,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          {{ $t('dhcpLedger.common.refresh') }}
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dhcpLedger.assets.create')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-subnets-layout">
      <OrgFilterCard
        :org-tree="orgTree"
        :selected-org-id="selectedOrgId"
        @select="onSelectOrgAndReload"
      />

      <div class="ipam-subnets-main">
        <div class="ipam-stat-grid">
          <StatCard
            :label="$t('dhcpLedger.assets.statTotal')"
            :value="stats.total"
            :unit="$t('dhcpLedger.common.units.item')"
            icon="lucide:contact"
            tone="blue"
            :hint="
              $t('dhcpLedger.assets.statTotalHint', { count: stats.owners })
            "
          />
          <StatCard
            :label="$t('dhcpLedger.assets.statAssigned')"
            :value="stats.assigned"
            :unit="$t('dhcpLedger.common.units.item')"
            icon="lucide:building-2"
            tone="cyan"
            :progress="
              stats.total > 0 ? (stats.assigned / stats.total) * 100 : 0
            "
            :hint="
              $t('dhcpLedger.assets.statAssignedHint', {
                count: stats.unassigned,
              })
            "
          />
          <StatCard
            :label="$t('dhcpLedger.assets.statOwners')"
            :value="stats.owners"
            :unit="$t('dhcpLedger.common.units.person')"
            icon="lucide:user-round"
            tone="green"
            :hint="
              $t('dhcpLedger.assets.statOwnersHint', { count: stats.depts })
            "
          />
          <StatCard
            :label="$t('dhcpLedger.assets.statOnline')"
            :value="stats.online"
            :unit="$t('dhcpLedger.common.units.device')"
            icon="lucide:activity"
            :progress="stats.total > 0 ? (stats.online / stats.total) * 100 : 0"
            :tone="stats.offline > 0 ? 'amber' : 'green'"
            :hint="
              $t('dhcpLedger.assets.statOnlineHint', { count: stats.offline })
            "
          />
        </div>

        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:clipboard-list" />
              {{ $t('dhcpLedger.assets.listCard') }}
              <em class="ipam-card-sub">
                {{
                  $t('dhcpLedger.assets.listCardSub', {
                    shown: rows.length,
                    total,
                  })
                }}
              </em>
            </span>
          </template>
          <template #extra>
            <span class="ipam-hint">{{
              $t('dhcpLedger.assets.listCardHint')
            }}</span>
          </template>

          <div class="ipam-toolbar mb-3">
            <div class="ipam-field">
              <span class="ipam-field__label">{{
                $t('dhcpLedger.common.keyword')
              }}</span>
              <Input
                v-model:value="keyword"
                allow-clear
                :placeholder="$t('dhcpLedger.assets.keywordPlaceholder')"
                style="width: 240px"
                @press-enter="applyFilters"
              />
            </div>
            <div class="ipam-field">
              <span class="ipam-field__label">{{
                $t('dhcpLedger.common.scope')
              }}</span>
              <Select
                v-model:value="scope"
                :options="scopeOptions"
                style="width: 150px"
                @change="applyFilters"
              />
            </div>
            <div class="ipam-field">
              <span class="ipam-field__label">{{
                $t('dhcpLedger.assets.onlineLabel')
              }}</span>
              <Select
                v-model:value="onlineFilter"
                :options="onlineOptions"
                style="width: 120px"
                @change="applyFilters"
              />
            </div>
            <Button size="small" @click="applyFilters">
              {{ $t('dhcpLedger.common.query') }}
            </Button>
            <Button size="small" @click="resetFilters">
              {{ $t('dhcpLedger.common.reset') }}
            </Button>
            <div class="ipam-toolbar__spacer"></div>
            <Button
              size="small"
              :disabled="rows.length === 0"
              @click="exportCsv"
            >
              <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />
              {{ $t('dhcpLedger.common.exportCsv') }}
            </Button>
            <span class="ipam-hint">{{
              $t('dhcpLedger.assets.hintDecoupled')
            }}</span>
          </div>

          <Table
            :data-source="rows"
            :loading="loading"
            :columns="assetColumns"
            :locale="{ emptyText }"
            :pagination="pagination"
            :row-key="(r: Asset) => r.identity"
            size="small"
            @change="onTableChange"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'mac'">
                <div class="ipam-cell-stack">
                  <span v-if="record.mac" class="ipam-mono">{{
                    record.mac
                  }}</span>
                  <span v-else class="ipam-chip" data-tone="violet">
                    {{ $t('dhcpLedger.assets.duidOnly') }}
                  </span>
                  <span
                    v-if="record.duid"
                    class="ipam-cell-meta ipam-mono"
                    :title="record.duid"
                  >
                    DUID {{ record.duid }}
                  </span>
                  <span v-if="record.note" class="ipam-cell-meta">
                    {{ record.note }}
                  </span>
                  <span v-else-if="!record.duid" class="ipam-cell-meta">
                    —
                  </span>
                </div>
              </template>
              <template v-else-if="column.dataIndex === 'ipv4'">
                <div class="ipam-cell-stack">
                  <span v-if="record.ipv4" class="ipam-mono">{{
                    record.ipv4
                  }}</span>
                  <span v-if="record.ipv6" class="ipam-mono ipam-cell-meta">{{
                    record.ipv6
                  }}</span>
                  <span
                    v-if="!record.ipv4 && !record.ipv6"
                    class="text-gray-400"
                  >
                    {{ $t('dhcpLedger.assets.noLease') }}
                  </span>
                </div>
              </template>
              <template v-else-if="column.dataIndex === 'online'">
                <Tag v-if="record.online" color="green" class="m-0">
                  {{ $t('dhcpLedger.assets.online') }}
                </Tag>
                <Tag v-else color="default" class="m-0">
                  {{ $t('dhcpLedger.assets.offline') }}
                </Tag>
              </template>
              <template v-else-if="column.dataIndex === 'owner'">
                <div class="ipam-cell-stack">
                  <span class="ipam-cell-title">{{ record.owner || '—' }}</span>
                  <span class="ipam-cell-meta">{{
                    record.dept || $t('dhcpLedger.assets.noDept')
                  }}</span>
                </div>
              </template>
              <template v-else-if="column.dataIndex === 'orgId'">
                <span
                  v-if="record.orgId"
                  class="ipam-org-path ipam-org-path--start"
                  :title="orgPathOf(record.orgId).join(' / ')"
                >
                  <template
                    v-for="(seg, i) in orgPathOf(record.orgId)"
                    :key="`${seg}-${i}`"
                  >
                    <IconifyIcon
                      v-if="i > 0"
                      icon="lucide:chevron-right"
                      class="size-3"
                    />
                    <b v-if="i === orgPathOf(record.orgId).length - 1">{{
                      seg
                    }}</b>
                    <span v-else>{{ seg }}</span>
                  </template>
                </span>
                <span v-else class="ipam-chip" data-tone="amber">{{
                  $t('dhcpLedger.assets.unassigned')
                }}</span>
              </template>
              <template v-else-if="column.dataIndex === 'tags'">
                <template v-if="record.tags?.length">
                  <Tag
                    v-for="t in record.tags"
                    :key="t"
                    color="blue"
                    class="m-0"
                  >
                    {{ t }}
                  </Tag>
                </template>
                <span v-else class="text-gray-400">—</span>
              </template>
              <template v-else-if="column.key === 'op'">
                <div class="flex items-center gap-1">
                  <Button size="small" @click="openEdit(record as Asset)">
                    {{ $t('dhcpLedger.common.edit') }}
                  </Button>
                  <Popconfirm
                    :title="$t('dhcpLedger.assets.deleteConfirmTitle')"
                    :description="$t('dhcpLedger.assets.deleteConfirmDesc')"
                    :ok-text="$t('dhcpLedger.common.delete')"
                    :cancel-text="$t('dhcpLedger.common.cancel')"
                    @confirm="remove(record.mac)"
                  >
                    <Button size="small" danger>
                      {{ $t('dhcpLedger.common.delete') }}
                    </Button>
                  </Popconfirm>
                </div>
              </template>
            </template>
          </Table>
        </Card>
      </div>
    </div>

    <FormModal class="w-[560px]">
      <div class="space-y-2">
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.mac')
          }}</span>
          <Input
            v-model:value="form.mac"
            :disabled="!!editingIdentity"
            :placeholder="$t('dhcpLedger.common.macExample')"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.duid')
          }}</span>
          <Input
            v-model:value="form.duid"
            :disabled="!!editingIdentity"
            :placeholder="$t('dhcpLedger.assets.form.duidPlaceholder')"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.owner')
          }}</span>
          <Input
            v-model:value="form.owner"
            :placeholder="$t('dhcpLedger.assets.form.ownerPlaceholder')"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.dept')
          }}</span>
          <Input
            v-model:value="form.dept"
            :placeholder="$t('dhcpLedger.assets.form.deptPlaceholder')"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.org')
          }}</span>
          <Select
            v-model:value="form.orgId"
            :placeholder="$t('dhcpLedger.assets.form.orgPlaceholder')"
            allow-clear
            :options="orgOptions"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.tags')
          }}</span>
          <Select
            v-model:value="form.tags"
            mode="tags"
            :placeholder="$t('dhcpLedger.assets.form.tagsPlaceholder')"
            :token-separators="[',']"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">{{
            $t('dhcpLedger.assets.form.note')
          }}</span>
          <Input
            v-model:value="form.note"
            :placeholder="$t('dhcpLedger.assets.form.notePlaceholder')"
            style="width: 300px"
          />
        </div>
      </div>
    </FormModal>
  </div>
</template>
