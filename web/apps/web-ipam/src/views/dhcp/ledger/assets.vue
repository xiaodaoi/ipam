<script setup lang="ts">
import type { Asset, OrgTreeNode } from '#/api/ipam';

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

// ── 资产清单 ──
const rows = ref<Asset[]>([]);
const loading = ref(false);
const keyword = ref('');
type Scope = 'all' | 'tagged' | 'unassigned';
const scope = ref<Scope>('all');
const scopeOptions = computed(() => [
  { label: $t('dhcpLedger.assets.scopeAll'), value: 'all' },
  { label: $t('dhcpLedger.assets.scopeUnassigned'), value: 'unassigned' },
  { label: $t('dhcpLedger.assets.scopeTagged'), value: 'tagged' },
]);

async function load() {
  loading.value = true;
  try {
    const res = await listAssets({
      orgId: selectedOrgId.value || undefined,
      q: keyword.value.trim() || undefined,
    });
    rows.value = res.items ?? [];
  } finally {
    loading.value = false;
  }
}

function resetFilters() {
  keyword.value = '';
  scope.value = 'all';
  void load();
}

const visibleRows = computed(() => {
  if (scope.value === 'unassigned') return rows.value.filter((r) => !r.orgId);
  if (scope.value === 'tagged')
    return rows.value.filter((r) => (r.tags ?? []).length > 0);
  return rows.value;
});

const assetColumns = computed(() => [
  {
    title: $t('dhcpLedger.assets.colTerminal'),
    dataIndex: 'mac',
    minWidth: 190,
  },
  {
    title: $t('dhcpLedger.assets.colOwnerDept'),
    dataIndex: 'owner',
    minWidth: 130,
  },
  { title: $t('dhcpLedger.assets.colOrg'), dataIndex: 'orgId', minWidth: 180 },
  { title: $t('dhcpLedger.assets.colTags'), dataIndex: 'tags', width: 130 },
  { title: $t('dhcpLedger.common.actions'), key: 'op', width: 110 },
]);

const emptyText = computed(() =>
  keyword.value || scope.value !== 'all'
    ? $t('dhcpLedger.assets.emptyFiltered')
    : $t('dhcpLedger.assets.emptyList'),
);

// ── 资产指标 ──
const stats = computed(() => {
  const total = rows.value.length;
  const assigned = rows.value.filter((r) => !!r.orgId).length;
  const owners = new Set(rows.value.map((r) => r.owner).filter(Boolean));
  const depts = new Set(rows.value.map((r) => r.dept).filter(Boolean));
  const tagged = rows.value.filter((r) => (r.tags ?? []).length > 0).length;
  const tags = new Set<string>();
  for (const r of rows.value) {
    for (const t of r.tags ?? []) tags.add(t);
  }
  return {
    assigned,
    deptCount: depts.size,
    ownerCount: owners.size,
    tagged,
    tagCount: tags.size,
    total,
    unassigned: total - assigned,
  };
});

// ── CSV 导出（当前筛选结果，含完整组织路径） ──
function csvCell(v: unknown): string {
  const s = String(v ?? '');
  const needsQuote = s.includes('"') || s.includes(',') || s.includes('\n');
  return needsQuote ? `"${s.replaceAll('"', '""')}"` : s;
}

function exportCsv() {
  const header = [
    $t('dhcpLedger.assets.csv.mac'),
    $t('dhcpLedger.assets.csv.owner'),
    $t('dhcpLedger.assets.csv.dept'),
    $t('dhcpLedger.assets.csv.org'),
    $t('dhcpLedger.assets.csv.tags'),
    $t('dhcpLedger.assets.csv.note'),
  ];
  const lines = visibleRows.value.map((r) =>
    [
      r.mac,
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
    $t('dhcpLedger.assets.exportDone', { count: visibleRows.value.length }),
  );
}

// ── 登记 / 编辑 ──
const editingMac = ref('');
const form = reactive({
  mac: '',
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
  editingMac.value = '';
  form.mac = '';
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
  editingMac.value = r.mac;
  form.mac = r.mac;
  form.owner = r.owner ?? '';
  form.dept = r.dept ?? '';
  form.orgId = r.orgId || undefined;
  form.tags = [...(r.tags ?? [])];
  form.note = r.note ?? '';
  formModalApi.setState({
    title: $t('dhcpLedger.assets.modalEditTitle', { mac: r.mac }),
    confirmText: $t('dhcpLedger.assets.modalSaveChanges'),
  });
  formModalApi.open();
}

async function save() {
  const mac = editingMac.value || normalizeMacInput(form.mac);
  if (!mac) {
    message.warning($t('dhcpLedger.common.invalidMac'));
    return;
  }
  if (!form.owner.trim()) {
    message.warning($t('dhcpLedger.assets.ownerRequired'));
    return;
  }
  try {
    await upsertAsset({
      mac,
      owner: form.owner.trim(),
      dept: form.dept.trim() || undefined,
      orgId: form.orgId || undefined,
      // 始终传数组：后端 tags 列为 NOT NULL，显式缺省会被编码为 NULL（详见 M2-048 修复）
      tags: [...form.tags],
      note: form.note.trim() || undefined,
    });
    message.success(
      editingMac.value
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

async function remove(mac: string) {
  try {
    await deleteAsset(mac);
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
              departments: stats.deptCount,
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
        @select="onSelectOrg"
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
              $t('dhcpLedger.assets.statTotalHint', { count: stats.ownerCount })
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
            :value="stats.ownerCount"
            :unit="$t('dhcpLedger.common.units.person')"
            icon="lucide:user-round"
            tone="green"
            :hint="
              $t('dhcpLedger.assets.statOwnersHint', { count: stats.deptCount })
            "
          />
          <StatCard
            :label="$t('dhcpLedger.assets.statTags')"
            :value="stats.tagged"
            :unit="$t('dhcpLedger.common.units.item')"
            icon="lucide:tags"
            :tone="stats.tagCount > 0 ? 'violet' : 'amber'"
            :hint="
              $t('dhcpLedger.assets.statTagsHint', { count: stats.tagCount })
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
                    shown: visibleRows.length,
                    total: rows.length,
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
                @press-enter="load"
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
              />
            </div>
            <Button size="small" @click="load">
              {{ $t('dhcpLedger.common.query') }}
            </Button>
            <Button size="small" @click="resetFilters">
              {{ $t('dhcpLedger.common.reset') }}
            </Button>
            <div class="ipam-toolbar__spacer"></div>
            <Button
              size="small"
              :disabled="visibleRows.length === 0"
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
            :data-source="visibleRows"
            :loading="loading"
            :columns="assetColumns"
            :locale="{ emptyText }"
            :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
            row-key="mac"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'mac'">
                <div class="ipam-cell-stack">
                  <span class="ipam-mono">{{ record.mac }}</span>
                  <span
                    v-if="record.note"
                    class="ipam-cell-meta"
                    :title="record.note"
                  >
                    {{ record.note }}
                  </span>
                  <span v-else class="ipam-cell-meta">—</span>
                </div>
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
            :disabled="!!editingMac"
            :placeholder="$t('dhcpLedger.common.macExample')"
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
