<script setup lang="ts">
import type { DualstackConflict, DualstackIdentity, DualstackMatchScheme, DualstackTemplate, Subnet } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Popconfirm, Select, Switch, Table, Tag, Tooltip } from 'ant-design-vue';

import { createDualstackIdentity, createDualstackTemplate, deleteDualstackIdentity, deleteDualstackTemplate, listDualstackConflicts, listDualstackIdentities, listDualstackTemplates, listSubnets, updateDualstackTemplate } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const showTotal = (t: number) => $t('dhcp.dualstack.showTotal', { n: t });
const rows = ref<DualstackTemplate[]>([]);
const loading = ref(false);

// 网段下拉：来自「子网管理」的 v4/v6 子网（M3-013 优化）
const subnets = ref<Subnet[]>([]);
const v4Options = computed(() =>
  subnets.value
    .filter((s) => s.family === 4)
    .map((s) => ({ value: s.cidr, label: s.name ? `${s.cidr}（${s.name}）` : s.cidr })),
);
const v6Options = computed(() =>
  subnets.value
    .filter((s) => s.family === 6)
    .map((s) => ({ value: s.cidr, label: s.name ? `${s.cidr}（${s.name}）` : s.cidr })),
);
async function loadSubnets() {
  try {
    const d = await listSubnets();
    subnets.value = d.items ?? [];
  } catch {
    subnets.value = [];
  }
}
const identityRows = ref<DualstackIdentity[]>([]);
const conflictRows = ref<DualstackConflict[]>([]);
const form = ref<{
  dnsSync: boolean; encoding: 'A' | 'B' | 'CUSTOM'; expr: string; graceHours: number;
  ipv4Cidr: string; ipv6Prefix: string; matchScheme: DualstackMatchScheme; name: string;
}>({
  name: '',
  ipv4Cidr: '',
  ipv6Prefix: '',
  encoding: 'B',
  expr: '{v4.hextet4}',
  dnsSync: true,
  graceHours: 24,
  matchScheme: 'auto',
});
const editingId = ref('');

async function load() {
  loading.value = true;
  try {
    const d = await listDualstackTemplates();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
async function loadIdentities() {
  const d = await listDualstackIdentities();
  identityRows.value = d.items ?? [];
}
async function loadConflicts() {
  const d = await listDualstackConflicts();
  conflictRows.value = d.items ?? [];
}
function loadAll() {
  void load();
  void loadIdentities();
  void loadConflicts();
  void loadSubnets();
}
function edit(r: DualstackTemplate) {
  editingId.value = r.id;
  formModalApi.setState({
    title: $t('dhcp.dualstack.modal.editTitle'),
    confirmText: $t('dhcp.dualstack.modal.saveEdit'),
  });
  formModalApi.open();
  form.value = {
    name: r.name, ipv4Cidr: r.ipv4Cidr, ipv6Prefix: r.ipv6Prefix, encoding: r.encoding,
    expr: r.expr, dnsSync: r.dnsSync ?? true, graceHours: r.graceHours ?? 24,
    matchScheme: r.matchScheme ?? 'auto',
  };
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: $t('dhcp.dualstack.modal.title'), confirmText: $t('dhcp.dualstack.modal.create'), onConfirm: () => add() });
function openAdd() {
  // 不能先 cancelEdit()（其 close() 异步，会在 open() 后把 isOpen 置回 false）
  editingId.value = '';
  form.value = { name: '', ipv4Cidr: '', ipv6Prefix: '', encoding: 'B', expr: '{v4.hextet4}', dnsSync: true, graceHours: 24, matchScheme: 'auto' };
  formModalApi.setState({
    title: $t('dhcp.dualstack.modal.newTitle'),
    confirmText: $t('dhcp.dualstack.modal.create'),
  });
  formModalApi.open();
}
function cancelEdit() {
  editingId.value = '';
  formModalApi.close();
  form.value = {
    name: '', ipv4Cidr: '', ipv6Prefix: '',
    encoding: 'B', expr: '{v4.hextet4}', dnsSync: true, graceHours: 24, matchScheme: 'auto',
  };
}
async function add() {
  if (!form.value.name || !form.value.ipv4Cidr || !form.value.ipv6Prefix) {
    message.warning($t('dhcp.dualstack.messages.required'));
    return;
  }
  if (!EXPR_OPTIONS.value.some((o) => o.value === form.value.expr)) {
    message.warning($t('dhcp.dualstack.messages.needExpr'));
    return;
  }
  if (form.value.graceHours < 0 || form.value.graceHours > 720) {
    message.warning($t('dhcp.dualstack.messages.graceRange'));
    return;
  }
  if (editingId.value) {
    await updateDualstackTemplate(editingId.value, { ...form.value });
    message.success($t('dhcp.dualstack.messages.templateUpdated'));
  } else {
    await createDualstackTemplate({ ...form.value });
    message.success($t('dhcp.dualstack.messages.templateCreated'));
  }
  cancelEdit();
  await load();
}
async function remove(id?: string) {
  if (id) await deleteDualstackTemplate(id);
  await load();
}

// ── MAC↔DUID 映射（M3-013）──
async function removeIdentity(mac: string) {
  await deleteDualstackIdentity(mac);
  message.success($t('dhcp.dualstack.messages.mappingDeleted'));
  await loadIdentities();
  await loadConflicts();
}

const mappingForm = ref<{ duid: string; mac: string; note: string }>({ mac: '', duid: '', note: '' });
const mappingMacOptions = ref<{ label: string; value: string; }[]>([]);
function openMapping(c: DualstackConflict) {
  mappingMacOptions.value = (c.v4Macs ?? []).map((m) => ({ value: m, label: m }));
  mappingForm.value = { mac: (c.v4Macs ?? [])[0] ?? '', duid: c.duid, note: '' };
  mappingModalApi.open();
}
const [MappingModal, mappingModalApi] = useVbenModal({ draggable: true, title: $t('dhcp.dualstack.mappingModal.title'), confirmText: $t('dhcp.dualstack.mappingModal.create'), onConfirm: () => submitMapping() });
async function submitMapping() {
  if (!mappingForm.value.mac || !mappingForm.value.duid) return;
  await createDualstackIdentity({
    mac: mappingForm.value.mac,
    duid: mappingForm.value.duid,
    note: mappingForm.value.note || undefined,
  });
  message.success($t('dhcp.dualstack.messages.mappingCreated'));
  mappingModalApi.close();
  await loadIdentities();
  await loadConflicts();
}
function canSetMapping(c: DualstackConflict): boolean {
  return (c.v4Macs?.length ?? 0) > 0;
}
function disableHint(c: DualstackConflict): string {
  if (c.reason === 'no_mac_signal')
    return $t('dhcp.dualstack.disableHint.noMac');
  if (c.reason === 'pinned_mismatch')
    return $t('dhcp.dualstack.disableHint.pinned');
  return $t('dhcp.dualstack.disableHint.none');
}

onMounted(loadAll);

const ENC_TEXT = computed<Record<string, string>>(() => ({
  B: $t('dhcp.dualstack.encText.B'),
  A: $t('dhcp.dualstack.encText.A'),
  CUSTOM: $t('dhcp.dualstack.encText.CUSTOM'),
}));
const EXAMPLE = computed(() => $t('dhcp.dualstack.example'));

// 仅列出后端 ApplyTemplate 已实现的两种；其余写法会报 unsupported expr（CUSTOM 待实现）
const EXPR_OPTIONS = computed(() => [
  {
    value: '{v4.hextet4}',
    label: `{v4.hextet4}${$t('dhcp.dualstack.exprHextet4Suffix')}`,
  },
  {
    value: '{v4.hex32}',
    label: `{v4.hex32}${$t('dhcp.dualstack.exprHex32Suffix')}`,
  },
]);

function onExprChange(v: unknown) {
  if (v === '{v4.hextet4}') form.value.encoding = 'B';
  else if (v === '{v4.hex32}') form.value.encoding = 'A';
}

const SCHEME_TEXT = computed<Record<DualstackMatchScheme, string>>(() => ({
  auto: $t('dhcp.dualstack.schemeText.auto'),
  option79: $t('dhcp.dualstack.schemeText.option79'),
  'client-id': $t('dhcp.dualstack.schemeText.client-id'),
  'duid-llt': $t('dhcp.dualstack.schemeText.duid-llt'),
  hostname: $t('dhcp.dualstack.schemeText.hostname'),
  admin: $t('dhcp.dualstack.schemeText.admin'),
}));
const SCHEME_HINT = computed<Record<DualstackMatchScheme, string>>(() => ({
  auto: $t('dhcp.dualstack.schemeHint.auto'),
  option79: $t('dhcp.dualstack.schemeHint.option79'),
  'client-id': $t('dhcp.dualstack.schemeHint.client-id'),
  'duid-llt': $t('dhcp.dualstack.schemeHint.duid-llt'),
  hostname: $t('dhcp.dualstack.schemeHint.hostname'),
  admin: $t('dhcp.dualstack.schemeHint.admin'),
}));
const SCHEME_OPTIONS = computed(() =>
  (['auto', 'option79', 'client-id', 'duid-llt', 'hostname', 'admin'] as const).map(
    (v) => ({ value: v, label: SCHEME_TEXT.value[v] }),
  ),
);
const SCHEME_COLOR: Record<string, string> = {
  auto: 'blue', admin: 'purple', option79: 'cyan', 'client-id': 'geekblue', 'duid-llt': 'green', hostname: 'orange',
};

function schemeText(v: unknown): string {
  if (typeof v !== 'string' || v === '') return 'auto';
  return SCHEME_TEXT.value[v as DualstackMatchScheme] ?? v;
}

const CONFLICT_TEXT = computed<Record<string, string>>(() => ({
  ambiguous_hostname: $t('dhcp.dualstack.conflictText.ambiguous_hostname'),
  no_mac_signal: $t('dhcp.dualstack.conflictText.no_mac_signal'),
  pinned_mismatch: $t('dhcp.dualstack.conflictText.pinned_mismatch'),
}));
const CONFLICT_COLOR: Record<string, string> = {
  ambiguous_hostname: 'orange',
  no_mac_signal: 'default',
  pinned_mismatch: 'red',
};
const SOURCE_TEXT = computed<Record<string, string>>(() => ({
  admin: $t('dhcp.dualstack.sourceText.admin'),
  auto: $t('dhcp.dualstack.sourceText.auto'),
}));

// ── 指标与筛选 ──
const templateStats = computed(() => {
  const total = rows.value.length;
  const dnsSync = rows.value.filter((r) => r.dnsSync).length;
  const auto = rows.value.filter((r) => (r.matchScheme ?? 'auto') === 'auto').length;
  return { auto, dnsSync, total };
});
const identityStats = computed(() => {
  const admin = identityRows.value.filter((r) => r.source === 'admin').length;
  return { admin, auto: identityRows.value.length - admin, total: identityRows.value.length };
});
const conflictStats = computed(() => {
  const pick = (reason: string) => conflictRows.value.filter((c) => c.reason === reason).length;
  return {
    ambiguous: pick('ambiguous_hostname'),
    noMac: pick('no_mac_signal'),
    pinned: pick('pinned_mismatch'),
    total: conflictRows.value.length,
  };
});

const tplKeyword = ref('');
type EncFilter = 'A' | 'all' | 'B';
const tplEncoding = ref<EncFilter>('all');
const tplScheme = ref<'all' | DualstackMatchScheme>('all');
const encOptions = computed(() => [
  { label: $t('dhcp.dualstack.encFilterAll'), value: 'all' },
  { label: $t('dhcp.dualstack.encB'), value: 'B' },
  { label: $t('dhcp.dualstack.encA'), value: 'A' },
]);
const schemeFilterOptions = computed(() => [
  { label: $t('dhcp.dualstack.schemeAll'), value: 'all' },
  ...SCHEME_OPTIONS.value,
]);

const filteredTemplates = computed(() => {
  const kw = tplKeyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (tplEncoding.value !== 'all' && r.encoding !== tplEncoding.value) return false;
    if (tplScheme.value !== 'all' && (r.matchScheme ?? 'auto') !== tplScheme.value) return false;
    if (!kw) return true;
    return `${r.name} ${r.ipv4Cidr} ${r.ipv6Prefix} ${r.expr}`.toLowerCase().includes(kw);
  });
});
function resetTplFilters() {
  tplKeyword.value = '';
  tplEncoding.value = 'all';
  tplScheme.value = 'all';
}

const idKeyword = ref('');
const idSource = ref<'admin' | 'all' | 'auto'>('all');
const sourceOptions = computed(() => [
  { label: $t('dhcp.dualstack.sourceAll'), value: 'all' },
  { label: $t('dhcp.dualstack.sourceAdmin'), value: 'admin' },
  { label: $t('dhcp.dualstack.sourceAuto'), value: 'auto' },
]);
const filteredIdentities = computed(() => {
  const kw = idKeyword.value.trim().toLowerCase();
  return identityRows.value.filter((r) => {
    if (idSource.value !== 'all' && r.source !== idSource.value) return false;
    if (!kw) return true;
    return `${r.mac} ${r.duid} ${r.note ?? ''}`.toLowerCase().includes(kw);
  });
});
function resetIdFilters() {
  idKeyword.value = '';
  idSource.value = 'all';
}

const cfReason = ref<'all' | DualstackConflict['reason']>('all');
const reasonOptions = computed(() => [
  { label: $t('dhcp.dualstack.reasonAll'), value: 'all' },
  { label: $t('dhcp.dualstack.conflictText.ambiguous_hostname'), value: 'ambiguous_hostname' },
  { label: $t('dhcp.dualstack.conflictText.no_mac_signal'), value: 'no_mac_signal' },
  { label: $t('dhcp.dualstack.conflictText.pinned_mismatch'), value: 'pinned_mismatch' },
]);
const filteredConflicts = computed(() =>
  cfReason.value === 'all'
    ? conflictRows.value
    : conflictRows.value.filter((c) => c.reason === cfReason.value),
);

// ── 表格列 / 空状态（随语言切换）──
const tplColumns = computed(() => [
  { title: $t('dhcp.dualstack.col.template'), dataIndex: 'name', minWidth: 260 },
  { title: $t('dhcp.dualstack.col.expr'), dataIndex: 'expr', minWidth: 220 },
  { title: $t('dhcp.dualstack.col.scheme'), dataIndex: 'matchScheme', width: 140 },
  { title: $t('dhcp.dualstack.col.runtime'), dataIndex: 'dnsSync', width: 150 },
  { title: $t('dhcp.dualstack.col.op'), key: 'op', width: 130 },
]);
const tplEmptyText = computed(() =>
  tplKeyword.value || tplEncoding.value !== 'all' || tplScheme.value !== 'all'
    ? $t('dhcp.dualstack.templateEmptyFiltered')
    : $t('dhcp.dualstack.templateEmpty'),
);

const identityColumns = computed(() => [
  { title: $t('dhcp.dualstack.col.identity'), dataIndex: 'mac' },
  { title: $t('dhcp.dualstack.col.source'), dataIndex: 'source', width: 76 },
  { title: $t('dhcp.dualstack.col.updatedAt'), dataIndex: 'updatedAt', width: 128 },
  { title: $t('dhcp.dualstack.col.op'), key: 'op', width: 66 },
]);
const identityEmptyText = computed(() =>
  idKeyword.value || idSource.value !== 'all'
    ? $t('dhcp.dualstack.identityEmptyFiltered')
    : $t('dhcp.dualstack.identityEmpty'),
);

const conflictColumns = computed(() => [
  { title: $t('dhcp.dualstack.col.hostname'), dataIndex: 'hostname' },
  { title: $t('dhcp.dualstack.col.macs'), dataIndex: 'v4Macs', width: 120 },
  { title: $t('dhcp.dualstack.col.reason'), dataIndex: 'reason', width: 100 },
  { title: $t('dhcp.dualstack.col.op'), key: 'op', width: 84 },
]);
const conflictEmptyText = computed(() =>
  cfReason.value === 'all'
    ? $t('dhcp.dualstack.conflictEmpty')
    : $t('dhcp.dualstack.conflictEmptyFiltered'),
);

function fmtShortTime(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  if (Number.isNaN(d.getTime()) || d.getFullYear() <= 1970) return '—';
  const p = (n: number) => `${n}`.padStart(2, '0');
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function fmtTime(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  if (Number.isNaN(d.getTime()) || d.getFullYear() <= 1970) return '—';
  return d.toLocaleString('zh-CN', { hour12: false });
}
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:layers"
      :title="$t('dhcp.dualstack.title')"
      :description="$t('dhcp.dualstack.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layout-template" class="size-3.5" />
          {{
            $t('dhcp.dualstack.meta', {
              templates: templateStats.total,
              conflicts: conflictStats.total,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="loadAll()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dhcp.common.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openAdd()">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dhcp.dualstack.newTemplate')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dhcp.dualstack.stat.templateLabel')"
        :value="templateStats.total"
        :unit="$t('dhcp.common.unitCount')"
        icon="lucide:layout-template"
        tone="blue"
        :hint="
          $t('dhcp.dualstack.stat.templateHint', { n: templateStats.auto })
        "
      />
      <StatCard
        :label="$t('dhcp.dualstack.stat.identityLabel')"
        :value="identityStats.total"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:fingerprint"
        tone="cyan"
        :hint="
          $t('dhcp.dualstack.stat.identityHint', {
            admin: identityStats.admin,
            auto: identityStats.auto,
          })
        "
      />
      <StatCard
        :label="$t('dhcp.dualstack.stat.conflictLabel')"
        :value="conflictStats.total"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:triangle-alert"
        :tone="conflictStats.total > 0 ? 'amber' : 'green'"
        :hint="
          conflictStats.total > 0
            ? $t('dhcp.dualstack.stat.conflictHint', {
                ambiguous: conflictStats.ambiguous,
                noMac: conflictStats.noMac,
                pinned: conflictStats.pinned,
              })
            : $t('dhcp.dualstack.stat.conflictOk')
        "
      />
      <StatCard
        :label="$t('dhcp.dualstack.stat.dnsLabel')"
        :value="templateStats.dnsSync"
        :unit="$t('dhcp.common.unitCount')"
        icon="lucide:refresh-cw"
        tone="violet"
        :progress="templateStats.total > 0 ? (templateStats.dnsSync / templateStats.total) * 100 : 0"
        :hint="
          $t('dhcp.dualstack.stat.dnsHint', {
            n: templateStats.total - templateStats.dnsSync,
          })
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:layout-template" />
          {{ $t('dhcp.dualstack.templateCard') }}
          <em class="ipam-card-sub">
            {{ filteredTemplates.length }} / {{ rows.length }}
            {{ $t('dhcp.common.unitCount') }}
          </em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dhcp.dualstack.templateExtra') }}</span>
      </template>

      <div class="ipam-summary mb-3">
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">
            {{ $t('dhcp.dualstack.summary.total') }}
          </span>
          <span class="ipam-summary__value">{{ templateStats.total }}<small>{{
              $t('dhcp.common.unitCount')
            }}</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">
            {{ $t('dhcp.dualstack.summary.auto') }}
          </span>
          <span class="ipam-summary__value">{{ templateStats.auto }}<small>{{
              $t('dhcp.common.unitCount')
            }}</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">
            {{ $t('dhcp.dualstack.summary.dnsSync') }}
          </span>
          <span class="ipam-summary__value">{{ templateStats.dnsSync }}<small>{{
              $t('dhcp.common.unitCount')
            }}</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">
            {{ $t('dhcp.dualstack.summary.conflicts') }}
          </span>
          <span class="ipam-summary__value">{{ conflictStats.total }}<small>{{
              $t('dhcp.common.unitRecord')
            }}</small></span>
        </div>
      </div>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dhcp.dualstack.keyword') }}</span>
          <Input
            v-model:value="tplKeyword"
            allow-clear
            :placeholder="$t('dhcp.dualstack.templateSearchPlaceholder')"
            style="width: 240px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dhcp.dualstack.encoding') }}</span>
          <Select v-model:value="tplEncoding" :options="encOptions" style="width: 140px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dhcp.dualstack.scheme') }}</span>
          <Select v-model:value="tplScheme" :options="schemeFilterOptions" style="width: 160px" />
        </div>
        <Button size="small" @click="resetTplFilters">
          {{ $t('dhcp.dualstack.reset') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dhcp.dualstack.encodingHint') }}</span>
      </div>

      <Table
        :data-source="filteredTemplates"
        :loading="loading"
        :columns="tplColumns"
        :locale="{ emptyText: tplEmptyText }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono">{{ record.ipv4Cidr }} → {{ record.ipv6Prefix }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'expr'">
            <div class="ipam-cell-stack">
              <Tag class="m-0" :color="record.encoding === 'B' ? 'blue' : 'cyan'">
                {{ ENC_TEXT[record.encoding] ?? record.encoding }}
              </Tag>
              <span class="ipam-cell-meta ipam-mono">{{ record.expr }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'matchScheme'">
            <Tag class="m-0" :color="SCHEME_COLOR[record.matchScheme] ?? 'default'">
              {{ schemeText(record.matchScheme) }}
            </Tag>
          </template>
          <template v-else-if="column.dataIndex === 'dnsSync'">
            <div class="flex flex-wrap gap-1">
              <span class="ipam-chip" :data-tone="record.dnsSync ? 'green' : 'muted'">
                DNS
                {{
                  record.dnsSync
                    ? $t('dhcp.dualstack.dnsOn')
                    : $t('dhcp.dualstack.dnsOff')
                }}
              </span>
              <span class="ipam-chip" data-tone="muted">
                {{ $t('dhcp.dualstack.grace', { h: record.graceHours ?? 24 }) }}
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(record as DualstackTemplate)">
                {{ $t('dhcp.common.edit') }}
              </Button>
              <Popconfirm
                :title="$t('dhcp.dualstack.confirmDeleteTemplate')"
                :description="$t('dhcp.dualstack.deleteDesc')"
                :ok-text="$t('dhcp.common.delete')"
                :cancel-text="$t('dhcp.common.cancel')"
                @confirm="remove(record.id)"
              >
                <Button size="small" danger>{{ $t('dhcp.common.delete') }}</Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <div class="ipam-dualstack-cols">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:fingerprint" />
            {{ $t('dhcp.dualstack.identitiesCard') }}
            <em class="ipam-card-sub">
              {{ filteredIdentities.length }} / {{ identityRows.length }}
              {{ $t('dhcp.common.unitRecord') }}
            </em>
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ $t('dhcp.dualstack.identitiesExtra') }}</span>
        </template>

        <div class="ipam-toolbar mb-3">
          <div class="ipam-field">
            <span class="ipam-field__label">{{ $t('dhcp.dualstack.keyword') }}</span>
            <Input
              v-model:value="idKeyword"
              allow-clear
              :placeholder="$t('dhcp.dualstack.identitySearchPlaceholder')"
              style="width: 190px"
            />
          </div>
          <div class="ipam-field">
            <span class="ipam-field__label">{{ $t('dhcp.dualstack.sourceLabel') }}</span>
            <Select v-model:value="idSource" :options="sourceOptions" style="width: 130px" />
          </div>
          <Button size="small" @click="resetIdFilters">
            {{ $t('dhcp.dualstack.reset') }}
          </Button>
        </div>

        <Table
          :data-source="filteredIdentities"
          :columns="identityColumns"
          :locale="{ emptyText: identityEmptyText }"
          :pagination="{ pageSize: 10, showSizeChanger: false, showTotal }"
          row-key="mac"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'mac'">
              <div class="ipam-cell-stack">
                <span class="ipam-cell-title ipam-mono">{{ record.mac }}</span>
                <span class="ipam-cell-meta ipam-mono">DUID {{ record.duid }}</span>
                <span v-if="record.note" class="ipam-cell-meta">{{ record.note }}</span>
              </div>
            </template>
            <template v-else-if="column.dataIndex === 'source'">
              <Tag class="m-0" :color="record.source === 'admin' ? 'blue' : 'default'">
                {{ SOURCE_TEXT[record.source] ?? record.source }}
              </Tag>
            </template>
            <template v-else-if="column.dataIndex === 'updatedAt'">
              <span class="ipam-hint">{{ fmtTime(record.updatedAt) }}</span>
            </template>
            <template v-else-if="column.key === 'op'">
              <Popconfirm
                :title="$t('dhcp.dualstack.confirmDeleteMapping')"
                :description="$t('dhcp.dualstack.deleteDesc')"
                :ok-text="$t('dhcp.common.delete')"
                :cancel-text="$t('dhcp.common.cancel')"
                @confirm="removeIdentity(record.mac)"
              >
                <Button size="small" danger>{{ $t('dhcp.common.delete') }}</Button>
              </Popconfirm>
            </template>
          </template>
        </Table>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:triangle-alert" />
            {{ $t('dhcp.dualstack.conflictsCard') }}
            <em class="ipam-card-sub">
              {{ filteredConflicts.length }} / {{ conflictRows.length }}
              {{ $t('dhcp.common.unitRecord') }}
            </em>
          </span>
        </template>
        <template #extra>
          <Tag v-if="conflictStats.total > 0" color="orange" class="m-0">
            {{ $t('dhcp.dualstack.needsConfirm') }}
          </Tag>
          <Tag v-else color="green" class="m-0">
            {{ $t('dhcp.dualstack.allResolved') }}
          </Tag>
        </template>

        <div class="ipam-toolbar mb-3">
          <div class="ipam-field">
            <span class="ipam-field__label">{{ $t('dhcp.dualstack.reasonLabel') }}</span>
            <Select v-model:value="cfReason" :options="reasonOptions" style="width: 160px" />
          </div>
          <div class="ipam-toolbar__spacer"></div>
          <span class="ipam-hint">{{ $t('dhcp.dualstack.reasonExtra') }}</span>
        </div>

        <Table
          :data-source="filteredConflicts"
          :columns="conflictColumns"
          :locale="{ emptyText: conflictEmptyText }"
          :pagination="{ pageSize: 10, showSizeChanger: false, showTotal }"
          row-key="duid"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'hostname'">
              <div class="ipam-cell-stack">
                <span class="ipam-cell-title">
                  {{ record.hostname || $t('dhcp.dualstack.unnamedTerminal') }}
                </span>
                <span class="ipam-cell-meta ipam-mono">
                  {{ record.v6Ip || $t('dhcp.dualstack.noV6') }}
                </span>
              </div>
            </template>
            <template v-else-if="column.dataIndex === 'v4Macs'">
              <div v-if="record.v4Macs?.length" class="flex flex-wrap gap-1">
                <span v-for="m in record.v4Macs" :key="m" class="ipam-chip ipam-mono" data-tone="cyan">{{ m }}</span>
              </div>
              <span v-else class="ipam-hint">
                {{ $t('dhcp.dualstack.noCandidate') }}
              </span>
            </template>
            <template v-else-if="column.dataIndex === 'reason'">
              <div class="ipam-cell-stack">
                <Tag class="m-0" :color="CONFLICT_COLOR[record.reason] ?? 'default'">
                  {{ CONFLICT_TEXT[record.reason] ?? record.reason }}
                </Tag>
                <span class="ipam-cell-meta">{{ fmtShortTime(record.updatedAt) }}</span>
              </div>
            </template>
            <template v-else-if="column.key === 'op'">
              <Button v-if="canSetMapping(record as DualstackConflict)" size="small" @click="openMapping(record as DualstackConflict)">
                {{ $t('dhcp.dualstack.setMapping') }}
              </Button>
              <Tooltip v-else :title="disableHint(record as DualstackConflict)">
                <span>
                  <Button size="small" disabled>
                    {{ $t('dhcp.dualstack.setMapping') }}
                  </Button>
                </span>
              </Tooltip>
            </template>
          </template>
        </Table>
      </Card>
    </div>

    <FormModal class="w-[880px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcp.dualstack.modal.name') }}<em>*</em></span>
          <Input
            v-model:value="form.name"
            :placeholder="$t('dhcp.dualstack.modal.namePlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcp.dualstack.modal.ipv4') }}<em>*</em></span>
          <Select
            v-model:value="form.ipv4Cidr"
            show-search
            :options="v4Options"
            :placeholder="$t('dhcp.dualstack.modal.ipv4Placeholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcp.dualstack.modal.ipv6') }}<em>*</em></span>
          <Select
            v-model:value="form.ipv6Prefix"
            show-search
            :options="v6Options"
            :placeholder="$t('dhcp.dualstack.modal.ipv6Placeholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.modal.encoding') }}
          </span>
          <Select v-model:value="form.encoding" :options="(['B', 'A'] as const).map((v) => ({ value: v, label: ENC_TEXT[v] }))" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.modal.expr') }}
          </span>
          <Select
            v-model:value="form.expr"
            :options="EXPR_OPTIONS"
            :placeholder="$t('dhcp.dualstack.modal.exprPlaceholder')"
            @change="onExprChange"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.modal.scheme') }}
          </span>
          <Select v-model:value="form.matchScheme" :options="SCHEME_OPTIONS" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.modal.grace') }}
          </span>
          <InputNumber v-model:value="form.graceHours" :min="0" :max="720" style="width: 100%" />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.modal.dnsSync') }}
          </span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.dnsSync" size="small" />
            <span class="ipam-hint">
              {{
                form.dnsSync
                  ? $t('dhcp.dualstack.modal.dnsSyncOn')
                  : $t('dhcp.dualstack.modal.dnsSyncOff')
              }}
            </span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{
            $t('dhcp.dualstack.exampleLine', {
              example: EXAMPLE,
              hint: SCHEME_HINT[form.matchScheme],
            })
          }}
        </div>
      </div>
    </FormModal>

    <MappingModal class="w-[460px]">
      <div class="flex flex-col gap-3">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.mappingModal.mac') }}
          </span>
          <Select v-if="mappingMacOptions.length > 1" v-model:value="mappingForm.mac" :options="mappingMacOptions" />
          <Input v-else v-model:value="mappingForm.mac" readonly />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">DUID</span>
          <Input v-model:value="mappingForm.duid" readonly />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.dualstack.mappingModal.note') }}
          </span>
          <Input
            v-model:value="mappingForm.note"
            :placeholder="$t('dhcp.dualstack.mappingModal.notePlaceholder')"
          />
        </label>
        <div class="ipam-form-hint">
          {{ $t('dhcp.dualstack.mappingModal.hint') }}
        </div>
      </div>
    </MappingModal>
  </div>
</template>
