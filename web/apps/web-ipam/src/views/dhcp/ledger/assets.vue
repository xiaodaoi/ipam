<script setup lang="ts">
import type { Asset, OrgTreeNode } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Popconfirm, Select, Table, Tag } from 'ant-design-vue';

import { deleteAsset, listAssets, listOrgTree, upsertAsset } from '#/api/ipam';
import OrgFilterCard from '#/components/org-filter-card.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { normalizeMacInput } from '#/utils/mac';

const showTotal = (t: number) => `共 ${t} 条`;

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
const scopeOptions = [
  { label: '全部资产', value: 'all' },
  { label: '未归属组织', value: 'unassigned' },
  { label: '已打标签', value: 'tagged' },
];

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
  if (scope.value === 'tagged') return rows.value.filter((r) => (r.tags ?? []).length > 0);
  return rows.value;
});

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
  return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
}

function exportCsv() {
  const header = ['MAC', '使用人', '部门', '所属组织', '标签', '备注'];
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
  message.success(`已导出 ${visibleRows.value.length} 条资产`);
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
  title: '登记资产',
  confirmText: '保存',
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
  formModalApi.setState({ title: '登记资产', confirmText: '登记' });
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
  formModalApi.setState({ title: `编辑资产：${r.mac}`, confirmText: '保存修改' });
  formModalApi.open();
}

async function save() {
  const mac = editingMac.value || normalizeMacInput(form.mac);
  if (!mac) {
    message.warning('请填写合法 MAC（支持 C4-3D-1A-07-EB-2B / C43D1A07EB2B / 冒号分隔，大小写均可）');
    return;
  }
  if (!form.owner.trim()) {
    message.warning('请填写使用人');
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
    message.success(editingMac.value ? '资产已更新' : '资产已登记');
    formModalApi.close();
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  }
}

async function remove(mac: string) {
  try {
    await deleteAsset(mac);
    message.success('资产已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
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
      title="资产登记"
      description="以 MAC 为身份键登记使用人 / 部门 / 组织 / 备注，终端换址不丢；为日志中心提供「终端 → 组织」关联链。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:contact-round" class="size-3.5" />
          {{ stats.total }} 条资产 · 覆盖 {{ stats.deptCount }} 个部门
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          刷新
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1" />登记资产
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
            label="资产总数"
            :value="stats.total"
            unit="条"
            icon="lucide:contact"
            tone="blue"
            :hint="`覆盖 ${stats.ownerCount} 位使用人`"
          />
          <StatCard
            label="已归属组织"
            :value="stats.assigned"
            unit="条"
            icon="lucide:building-2"
            tone="cyan"
            :progress="stats.total > 0 ? (stats.assigned / stats.total) * 100 : 0"
            :hint="`未归属 ${stats.unassigned} 条`"
          />
          <StatCard
            label="使用人"
            :value="stats.ownerCount"
            unit="位"
            icon="lucide:user-round"
            tone="green"
            :hint="`按使用人去重 · ${stats.deptCount} 个部门`"
          />
          <StatCard
            label="标签覆盖"
            :value="stats.tagged"
            unit="条"
            icon="lucide:tags"
            :tone="stats.tagCount > 0 ? 'violet' : 'amber'"
            :hint="`共 ${stats.tagCount} 个不同标签`"
          />
        </div>

        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:clipboard-list" />
              资产清单
              <em class="ipam-card-sub">{{ visibleRows.length }} / {{ rows.length }} 条</em>
            </span>
          </template>
          <template #extra>
            <span class="ipam-hint">以 MAC 幂等登记 · 与 IP 解耦</span>
          </template>

          <div class="ipam-toolbar mb-3">
            <div class="ipam-field">
              <span class="ipam-field__label">关键字</span>
              <Input
                v-model:value="keyword"
                allow-clear
                placeholder="MAC / 使用人 / 部门"
                style="width: 240px"
                @press-enter="load"
              />
            </div>
            <div class="ipam-field">
              <span class="ipam-field__label">范围</span>
              <Select v-model:value="scope" :options="scopeOptions" style="width: 150px" />
            </div>
            <Button size="small" @click="load">查询</Button>
            <Button size="small" @click="resetFilters">重置</Button>
            <div class="ipam-toolbar__spacer"></div>
            <Button size="small" :disabled="visibleRows.length === 0" @click="exportCsv">
              <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />
              导出 CSV
            </Button>
            <span class="ipam-hint">登记信息与 IP 解耦，终端换址后保持不变</span>
          </div>

          <Table
            :data-source="visibleRows"
            :loading="loading"
            :columns="[
              { title: '终端（MAC / 备注）', dataIndex: 'mac', minWidth: 190 },
              { title: '使用人 / 部门', dataIndex: 'owner', minWidth: 130 },
              { title: '所属组织', dataIndex: 'orgId', minWidth: 180 },
              { title: '标签', dataIndex: 'tags', width: 130 },
              { title: '操作', key: 'op', width: 110 },
            ]"
            :locale="{ emptyText: keyword || scope !== 'all' ? '没有匹配的资产登记' : '当前组织暂无资产，点击右上角「登记资产」添加' }"
            :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
            row-key="mac"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'mac'">
                <div class="ipam-cell-stack">
                  <span class="ipam-mono">{{ record.mac }}</span>
                  <span v-if="record.note" class="ipam-cell-meta" :title="record.note">
                    {{ record.note }}
                  </span>
                  <span v-else class="ipam-cell-meta">—</span>
                </div>
              </template>
              <template v-else-if="column.dataIndex === 'owner'">
                <div class="ipam-cell-stack">
                  <span class="ipam-cell-title">{{ record.owner || '—' }}</span>
                  <span class="ipam-cell-meta">{{ record.dept || '未填写部门' }}</span>
                </div>
              </template>
              <template v-else-if="column.dataIndex === 'orgId'">
                <span
                  v-if="record.orgId"
                  class="ipam-org-path ipam-org-path--start"
                  :title="orgPathOf(record.orgId).join(' / ')"
                >
                  <template v-for="(seg, i) in orgPathOf(record.orgId)" :key="`${seg}-${i}`">
                    <IconifyIcon v-if="i > 0" icon="lucide:chevron-right" class="size-3" />
                    <b v-if="i === orgPathOf(record.orgId).length - 1">{{ seg }}</b>
                    <span v-else>{{ seg }}</span>
                  </template>
                </span>
                <span v-else class="ipam-chip" data-tone="amber">未归属</span>
              </template>
              <template v-else-if="column.dataIndex === 'tags'">
                <template v-if="record.tags?.length">
                  <Tag v-for="t in record.tags" :key="t" color="blue" class="m-0">{{ t }}</Tag>
                </template>
                <span v-else class="text-gray-400">—</span>
              </template>
              <template v-else-if="column.key === 'op'">
                <div class="flex items-center gap-1">
                  <Button size="small" @click="openEdit(record as Asset)">编辑</Button>
                  <Popconfirm
                    title="确认删除该资产登记？"
                    description="删除后使用人 / 部门 / 备注将丢失（不影响 DHCP 下发）。"
                    ok-text="删除"
                    cancel-text="取消"
                    @confirm="remove(record.mac)"
                  >
                    <Button size="small" danger>删除</Button>
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
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">MAC 地址</span>
          <Input
            v-model:value="form.mac"
            :disabled="!!editingMac"
            placeholder="如 C4-3D-1A-07-EB-2B"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">使用人</span>
          <Input v-model:value="form.owner" placeholder="使用人 / 责任人" style="width: 300px" />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">部门</span>
          <Input v-model:value="form.dept" placeholder="部门（可选）" style="width: 300px" />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">所属组织</span>
          <Select
            v-model:value="form.orgId"
            placeholder="选择组织（可选）"
            allow-clear
            :options="orgOptions"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">标签</span>
          <Select
            v-model:value="form.tags"
            mode="tags"
            placeholder="回车添加标签（可选）"
            :token-separators="[',']"
            style="width: 300px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-right text-xs text-gray-400">备注</span>
          <Input v-model:value="form.note" placeholder="备注（可选）" style="width: 300px" />
        </div>
      </div>
    </FormModal>
  </div>
</template>
