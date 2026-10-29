<script setup lang="ts">
import type { ForwardRule, Upstream } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Popconfirm, Select, Switch, Table } from 'ant-design-vue';

import { createForwardRule, deleteForwardRule, listForwardRules, listUpstreams, updateForwardRule } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const showTotal = (t: number) => `共 ${t} 条`;

const rows = ref<ForwardRule[]>([]);
const upstreams = ref<Upstream[]>([]);
const loading = ref(false);
const editingId = ref<string>();
const form = ref({ domain: '', upstreamId: '', note: '' });

async function load() {
  loading.value = true;
  try {
    const [fr, up] = await Promise.all([listForwardRules(), listUpstreams()]);
    rows.value = fr.items ?? [];
    upstreams.value = up.items ?? [];
  } finally {
    loading.value = false;
  }
}
function upstreamName(id: string): string {
  return upstreams.value.find((u) => u.id === id)?.name ?? id.slice(0, 8);
}
async function add() {
  if (!form.value.domain || !form.value.upstreamId) {
    message.warning('域名后缀与转发上游必填');
    return;
  }
  const domain = form.value.domain.endsWith('.') ? form.value.domain : `${form.value.domain}.`;
  try {
    if (editingId.value) {
      await updateForwardRule(editingId.value, {
        upstreamIds: [form.value.upstreamId],
        note: form.value.note,
      });
      message.success('转发规则已更新');
    } else {
      await createForwardRule({
        domain,
        upstreamIds: [form.value.upstreamId],
        enabled: true,
        note: form.value.note,
      });
      message.success('转发规则已创建');
    }
    editingId.value = undefined;
    form.value = { domain: '', upstreamId: '', note: '' };
    formModalApi.close();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '转发规则', confirmText: '添加', onConfirm: () => add() });
function openAdd() {
  editingId.value = undefined;
  form.value = { domain: '', upstreamId: '', note: '' };
  formModalApi.setState({ title: '添加转发规则', confirmText: '创建规则' });
  formModalApi.open();
}
function edit(r: ForwardRule) {
  editingId.value = r.id;
  formModalApi.setState({ title: '编辑转发规则', confirmText: '保存修改' });
  formModalApi.open();
  form.value = {
    domain: r.domain,
    upstreamId: (r.upstreamIds ?? [])[0] ?? '',
    note: r.note ?? '',
  };
}
async function toggleEnabled(r: ForwardRule, checked: boolean) {
  try {
    await updateForwardRule(r.id, { enabled: checked });
    message.success(checked ? '已启用' : '已停用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteForwardRule(id);
    message.success('转发规则已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}
onMounted(load);

// ── 指标 ──
const stats = computed(() => {
  const total = rows.value.length;
  const enabled = rows.value.filter((r) => r.enabled).length;
  const linked = new Set(rows.value.flatMap((r) => r.upstreamIds ?? [])).size;
  const defaults = rows.value.filter((r) => r.domain === '.').length;
  const linkedIds = new Set(rows.value.flatMap((r) => r.upstreamIds ?? []));
  const linkedOnline = upstreams.value.filter((u) => linkedIds.has(u.id) && u.health?.up === true).length;
  const linkedDown = upstreams.value.filter((u) => linkedIds.has(u.id) && u.health?.up === false).length;
  return { defaults, enabled, linked, linkedDown, linkedOnline, total };
});

// ── 筛选 ──
const keyword = ref('');
const statusFilter = ref<'all' | 'disabled' | 'enabled'>('all');
const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '已启用', value: 'enabled' },
  { label: '已停用', value: 'disabled' },
];
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (statusFilter.value === 'enabled' && !r.enabled) return false;
    if (statusFilter.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    const names = (r.upstreamIds ?? []).map((id) => upstreamName(id)).join(' ');
    return `${r.domain} ${r.note ?? ''} ${names}`.toLowerCase().includes(kw);
  });
});
function resetFilters() {
  keyword.value = '';
  statusFilter.value = 'all';
}
const upstreamOptions = computed(() =>
  upstreams.value.map((u) => ({
    value: u.id,
    label: `${u.name}${u.health?.up === false ? '（已摘除）' : ''}`,
  })),
);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:route"
      title="条件转发规则"
      description="按域名后缀将查询转发到指定上游，最长后缀优先匹配，未命中时回落到默认上游。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:route" class="size-3.5" />
          {{ stats.total }} 条规则 · {{ stats.enabled }} 条启用
        </span>
        <Button size="small" :loading="loading" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openAdd">
          <IconifyIcon icon="lucide:plus" class="mr-1" />添加转发规则
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="转发规则"
        :value="stats.total"
        unit="条"
        icon="lucide:route"
        tone="blue"
        hint="最长后缀优先匹配"
      />
      <StatCard
        label="已启用规则"
        :value="stats.enabled"
        unit="条"
        icon="lucide:toggle-right"
        tone="green"
        :progress="stats.total > 0 ? (stats.enabled / stats.total) * 100 : 0"
        :hint="stats.total === 0 ? '尚未配置转发规则' : `停用 ${stats.total - stats.enabled} 条不参与选路`"
      />
      <StatCard
        label="关联上游"
        :value="stats.linked"
        unit="个"
        icon="lucide:server"
        tone="cyan"
        :hint="stats.linked > 0 ? `在线 ${stats.linkedOnline} · 已摘除 ${stats.linkedDown}` : '按上游健康状态选路'"
      />
      <StatCard
        label="默认后缀"
        :value="stats.defaults"
        unit="条"
        icon="lucide:asterisk"
        tone="violet"
        :hint="stats.defaults > 0 ? '根域名「.」承接全部未命中查询' : '未配置默认转发，走递归'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-tree" />
          转发规则列表
          <em class="ipam-card-sub">{{ filtered.length }} / {{ rows.length }} 条</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">未命中规则的查询走默认上游</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input v-model:value="keyword" allow-clear placeholder="域名后缀 / 备注 / 上游" style="width: 260px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">状态</span>
          <Select v-model:value="statusFilter" :options="statusOptions" style="width: 130px" />
        </div>
        <Button size="small" @click="resetFilters">重置</Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">域名后缀保存时自动补尾点</span>
      </div>

      <Table
        :data-source="filtered"
        :loading="loading"
        :columns="[
          { title: '域名后缀 / 备注', dataIndex: 'domain', minWidth: 280 },
          { title: '转发上游', dataIndex: 'upstreamIds', minWidth: 220 },
          { title: '启用', dataIndex: 'enabled', width: 90 },
          { title: '操作', key: 'op', width: 140 },
        ]"
        :locale="{ emptyText: keyword || statusFilter !== 'all' ? '没有匹配的转发规则' : '暂无转发规则，点击右上角「添加转发规则」创建' }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'domain'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title ipam-mono">{{ record.domain }}</span>
              <span class="ipam-cell-meta">{{ record.note || (record.domain === '.' ? '默认转发（全部未命中查询）' : '未填写备注') }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'upstreamIds'">
            <div v-if="(record.upstreamIds ?? []).length > 0" class="flex flex-wrap gap-1">
              <span v-for="id in record.upstreamIds" :key="id" class="ipam-chip" data-tone="blue">
                {{ upstreamName(id) }}
              </span>
            </div>
            <span v-else class="ipam-hint">未指定上游</span>
          </template>
          <template v-else-if="column.dataIndex === 'enabled'">
            <Switch :checked="record.enabled" size="small" @change="(v: any) => toggleEnabled(record as ForwardRule, !!v)" />
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(record as ForwardRule)">编辑</Button>
              <Popconfirm title="确认删除该转发规则？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="remove(record.id)">
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>

      <div class="ipam-note mt-3">
        <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        <span>匹配顺序按后缀长度降序；同一后缀只保留一条规则，编辑时仅允许调整上游与备注。</span>
      </div>
    </Card>

    <FormModal class="w-[640px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">域名后缀<em>*</em></span>
          <Input v-model:value="form.domain" placeholder="如 corp.local（最长后缀优先匹配，可用 . 表示默认）" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">转发上游<em>*</em></span>
          <Select v-model:value="form.upstreamId" placeholder="选择上游 DNS" show-search :options="upstreamOptions" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">备注</span>
          <Input v-model:value="form.note" placeholder="可选，说明该后缀的业务用途" />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          未填写尾点时会自动补全为 FQDN；规则保存后即时下发到 resolver。
        </div>
      </div>
    </FormModal>
  </div>
</template>
