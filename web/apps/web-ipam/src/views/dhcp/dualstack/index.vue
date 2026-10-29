<script setup lang="ts">
import type { DualstackConflict, DualstackIdentity, DualstackMatchScheme, DualstackTemplate, Subnet } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Popconfirm, Select, Switch, Table, Tag, Tooltip } from 'ant-design-vue';

import { createDualstackIdentity, createDualstackTemplate, deleteDualstackIdentity, deleteDualstackTemplate, listDualstackConflicts, listDualstackIdentities, listDualstackTemplates, listSubnets, updateDualstackTemplate } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const showTotal = (t: number) => `共 ${t} 条`;
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
  formModalApi.setState({ title: '编辑模板', confirmText: '保存修改' });
  formModalApi.open();
  form.value = {
    name: r.name, ipv4Cidr: r.ipv4Cidr, ipv6Prefix: r.ipv6Prefix, encoding: r.encoding,
    expr: r.expr, dnsSync: r.dnsSync ?? true, graceHours: r.graceHours ?? 24,
    matchScheme: r.matchScheme ?? 'auto',
  };
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '双栈绑定模板', confirmText: '创建模板', onConfirm: () => add() });
function openAdd() {
  // 不能先 cancelEdit()（其 close() 异步，会在 open() 后把 isOpen 置回 false）
  editingId.value = '';
  form.value = { name: '', ipv4Cidr: '', ipv6Prefix: '', encoding: 'B', expr: '{v4.hextet4}', dnsSync: true, graceHours: 24, matchScheme: 'auto' };
  formModalApi.setState({ title: '新建双栈绑定模板', confirmText: '创建模板' });
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
    message.warning('名称、IPv4 网段、IPv6 前缀均为必填');
    return;
  }
  if (!EXPR_OPTIONS.some((o) => o.value === form.value.expr)) {
    message.warning('请选择表达式');
    return;
  }
  if (form.value.graceHours < 0 || form.value.graceHours > 720) {
    message.warning('宽限期需在 0–720 小时之间');
    return;
  }
  if (editingId.value) {
    await updateDualstackTemplate(editingId.value, { ...form.value });
    message.success('模板已更新');
  } else {
    await createDualstackTemplate({ ...form.value });
    message.success('模板已创建');
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
  message.success('映射已删除');
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
const [MappingModal, mappingModalApi] = useVbenModal({ draggable: true, title: '设为 MAC↔DUID 映射', confirmText: '创建映射', onConfirm: () => submitMapping() });
async function submitMapping() {
  if (!mappingForm.value.mac || !mappingForm.value.duid) return;
  await createDualstackIdentity({
    mac: mappingForm.value.mac,
    duid: mappingForm.value.duid,
    note: mappingForm.value.note || undefined,
  });
  message.success('映射已创建');
  mappingModalApi.close();
  await loadIdentities();
  await loadConflicts();
}
function canSetMapping(c: DualstackConflict): boolean {
  return (c.v4Macs?.length ?? 0) > 0;
}
function disableHint(c: DualstackConflict): string {
  if (c.reason === 'no_mac_signal') return '该冲突无 MAC 信号，无可选候选';
  if (c.reason === 'pinned_mismatch') return '钉死方案回落且无候选 MAC，需先在 v4 侧补充信号';
  return '无候选 MAC';
}

onMounted(loadAll);

const ENC_TEXT: Record<string, string> = { B: 'B 型', A: 'A 型', CUSTOM: '自定义' };
const EXAMPLE = '例：192.168.0.10 → 2407::192:168:0:10';

// 仅列出后端 ApplyTemplate 已实现的两种；其余写法会报 unsupported expr（CUSTOM 待实现）
const EXPR_OPTIONS = [
  { value: '{v4.hextet4}', label: '{v4.hextet4}（B 型 · 十进制镜像，地址可读）' },
  { value: '{v4.hex32}', label: '{v4.hex32}（A 型 · v4 压入末 32 位，紧凑）' },
];

function onExprChange(v: unknown) {
  if (v === '{v4.hextet4}') form.value.encoding = 'B';
  else if (v === '{v4.hex32}') form.value.encoding = 'A';
}

const SCHEME_TEXT: Record<DualstackMatchScheme, string> = {
  auto: '自动（优先级链）',
  option79: '中继 option79',
  'client-id': 'RFC4361',
  'duid-llt': 'DUID 内嵌 MAC',
  hostname: '主机名',
  admin: '人工映射',
};
const SCHEME_HINT: Record<DualstackMatchScheme, string> = {
  auto: '按 admin→option79→client-id→duid-llt→hostname 优先级链自动关联',
  option79: '需中继开 RFC6939 且 kea hook 已加载',
  'client-id': '需客户端 DHCPv4 携带 RFC4361 client-id（即 DUID）',
  'duid-llt': '从 DUID-LLT 内嵌 MAC 提取关联',
  hostname: '按主机名反查 v4 租约，同名歧义进冲突清单',
  admin: '仅认人工映射，未建映射进冲突清单',
};
const SCHEME_OPTIONS = (['auto', 'option79', 'client-id', 'duid-llt', 'hostname', 'admin'] as const).map(
  (v) => ({ value: v, label: SCHEME_TEXT[v] }),
);
const SCHEME_COLOR: Record<string, string> = {
  auto: 'blue', admin: 'purple', option79: 'cyan', 'client-id': 'geekblue', 'duid-llt': 'green', hostname: 'orange',
};

function schemeText(v: unknown): string {
  if (typeof v !== 'string' || v === '') return 'auto';
  return SCHEME_TEXT[v as DualstackMatchScheme] ?? v;
}

const CONFLICT_TEXT: Record<string, string> = {
  ambiguous_hostname: '同名歧义',
  no_mac_signal: '无 MAC 信号',
  pinned_mismatch: '钉死方案回落',
};
const CONFLICT_COLOR: Record<string, string> = {
  ambiguous_hostname: 'orange',
  no_mac_signal: 'default',
  pinned_mismatch: 'red',
};
const SOURCE_TEXT: Record<string, string> = { admin: '人工', auto: '自动' };

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
const encOptions = [
  { label: '全部编码', value: 'all' },
  { label: 'B 型（可读）', value: 'B' },
  { label: 'A 型（紧凑）', value: 'A' },
];
const schemeFilterOptions = [
  { label: '全部匹配方案', value: 'all' },
  ...SCHEME_OPTIONS,
];

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
const sourceOptions = [
  { label: '全部来源', value: 'all' },
  { label: '人工确认', value: 'admin' },
  { label: '自动学习', value: 'auto' },
];
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
const reasonOptions = [
  { label: '全部原因', value: 'all' },
  { label: '同名歧义', value: 'ambiguous_hostname' },
  { label: '无 MAC 信号', value: 'no_mac_signal' },
  { label: '钉死方案回落', value: 'pinned_mismatch' },
];
const filteredConflicts = computed(() =>
  cfReason.value === 'all'
    ? conflictRows.value
    : conflictRows.value.filter((c) => c.reason === cfReason.value),
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
      title="双栈管理"
      description="配置 IPv4 地址池与 IPv6 前缀的绑定映射，维护 MAC↔DUID 身份并处理未解析冲突。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layout-template" class="size-3.5" />
          {{ templateStats.total }} 个模板 · {{ conflictStats.total }} 条冲突
        </span>
        <Button size="small" :loading="loading" @click="loadAll()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openAdd()">
          <IconifyIcon icon="lucide:plus" class="mr-1" />新建模板
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="绑定模板"
        :value="templateStats.total"
        unit="个"
        icon="lucide:layout-template"
        tone="blue"
        :hint="`${templateStats.auto} 个走自动优先级链`"
      />
      <StatCard
        label="身份映射"
        :value="identityStats.total"
        unit="条"
        icon="lucide:fingerprint"
        tone="cyan"
        :hint="`人工 ${identityStats.admin} · 自动 ${identityStats.auto}`"
      />
      <StatCard
        label="未解析冲突"
        :value="conflictStats.total"
        unit="条"
        icon="lucide:triangle-alert"
        :tone="conflictStats.total > 0 ? 'amber' : 'green'"
        :hint="conflictStats.total > 0 ? `同名 ${conflictStats.ambiguous} · 无信号 ${conflictStats.noMac} · 回落 ${conflictStats.pinned}` : '当前没有待处理冲突'"
      />
      <StatCard
        label="DNS 同步模板"
        :value="templateStats.dnsSync"
        unit="个"
        icon="lucide:refresh-cw"
        tone="violet"
        :progress="templateStats.total > 0 ? (templateStats.dnsSync / templateStats.total) * 100 : 0"
        :hint="`${templateStats.total - templateStats.dnsSync} 个模板未开启同步`"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:layout-template" />
          双栈绑定模板
          <em class="ipam-card-sub">{{ filteredTemplates.length }} / {{ rows.length }} 个</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">daemon 按租约 IPv4 最长前缀自动选择模板</span>
      </template>

      <div class="ipam-summary mb-3">
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">模板总数</span>
          <span class="ipam-summary__value">{{ templateStats.total }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">自动匹配</span>
          <span class="ipam-summary__value">{{ templateStats.auto }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">DNS 同步</span>
          <span class="ipam-summary__value">{{ templateStats.dnsSync }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">未解析冲突</span>
          <span class="ipam-summary__value">{{ conflictStats.total }}<small>条</small></span>
        </div>
      </div>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input
            v-model:value="tplKeyword"
            allow-clear
            placeholder="名称 / IPv4 / IPv6 / 表达式"
            style="width: 240px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">编码</span>
          <Select v-model:value="tplEncoding" :options="encOptions" style="width: 140px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">匹配方案</span>
          <Select v-model:value="tplScheme" :options="schemeFilterOptions" style="width: 160px" />
        </div>
        <Button size="small" @click="resetTplFilters">重置</Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">A 型压入末 32 位 · B 型十进制镜像</span>
      </div>

      <Table
        :data-source="filteredTemplates"
        :loading="loading"
        :columns="[
          { title: '模板（IPv4 → IPv6）', dataIndex: 'name', minWidth: 260 },
          { title: '编码 / 表达式', dataIndex: 'expr', minWidth: 220 },
          { title: '匹配方案', dataIndex: 'matchScheme', width: 140 },
          { title: '运行参数', dataIndex: 'dnsSync', width: 150 },
          { title: '操作', key: 'op', width: 130 },
        ]"
        :locale="{ emptyText: tplKeyword || tplEncoding !== 'all' || tplScheme !== 'all' ? '没有匹配的模板' : '暂无绑定模板，点击右上角「新建模板」创建' }"
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
                DNS {{ record.dnsSync ? '同步' : '不同步' }}
              </span>
              <span class="ipam-chip" data-tone="muted">宽限 {{ record.graceHours ?? 24 }}h</span>
            </div>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(record as DualstackTemplate)">编辑</Button>
              <Popconfirm title="确认删除该模板？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="remove(record.id)">
                <Button size="small" danger>删除</Button>
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
            MAC ↔ DUID 映射
            <em class="ipam-card-sub">{{ filteredIdentities.length }} / {{ identityRows.length }} 条</em>
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">人工维护 + 自动学习</span>
        </template>

        <div class="ipam-toolbar mb-3">
          <div class="ipam-field">
            <span class="ipam-field__label">关键字</span>
            <Input v-model:value="idKeyword" allow-clear placeholder="MAC / DUID / 备注" style="width: 190px" />
          </div>
          <div class="ipam-field">
            <span class="ipam-field__label">来源</span>
            <Select v-model:value="idSource" :options="sourceOptions" style="width: 130px" />
          </div>
          <Button size="small" @click="resetIdFilters">重置</Button>
        </div>

        <Table
          :data-source="filteredIdentities"
          :columns="[
            { title: '身份（MAC / DUID）', dataIndex: 'mac' },
            { title: '来源', dataIndex: 'source', width: 76 },
            { title: '更新时间', dataIndex: 'updatedAt', width: 128 },
            { title: '操作', key: 'op', width: 66 },
          ]"
          :locale="{ emptyText: idKeyword || idSource !== 'all' ? '没有匹配的身份映射' : '暂无映射，冲突处理时人工建立或由匹配方案自动学习' }"
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
              <Popconfirm title="确认删除该映射？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="removeIdentity(record.mac)">
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            </template>
          </template>
        </Table>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:triangle-alert" />
            未解析冲突
            <em class="ipam-card-sub">{{ filteredConflicts.length }} / {{ conflictRows.length }} 条</em>
          </span>
        </template>
        <template #extra>
          <Tag v-if="conflictStats.total > 0" color="orange" class="m-0">需人工确认</Tag>
          <Tag v-else color="green" class="m-0">全部已解析</Tag>
        </template>

        <div class="ipam-toolbar mb-3">
          <div class="ipam-field">
            <span class="ipam-field__label">原因</span>
            <Select v-model:value="cfReason" :options="reasonOptions" style="width: 160px" />
          </div>
          <div class="ipam-toolbar__spacer"></div>
          <span class="ipam-hint">为冲突选择 v4 MAC 后建立映射</span>
        </div>

        <Table
          :data-source="filteredConflicts"
          :columns="[
            { title: '终端（主机名 / v6 地址）', dataIndex: 'hostname' },
            { title: 'v4 MAC 候选', dataIndex: 'v4Macs', width: 120 },
            { title: '原因 / 时间', dataIndex: 'reason', width: 100 },
            { title: '操作', key: 'op', width: 84 },
          ]"
          :locale="{ emptyText: cfReason !== 'all' ? '该原因下没有冲突' : '暂无未解析冲突，双栈关联健康' }"
          :pagination="{ pageSize: 10, showSizeChanger: false, showTotal }"
          row-key="duid"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'hostname'">
              <div class="ipam-cell-stack">
                <span class="ipam-cell-title">{{ record.hostname || '未命名终端' }}</span>
                <span class="ipam-cell-meta ipam-mono">{{ record.v6Ip || '无 v6 地址' }}</span>
              </div>
            </template>
            <template v-else-if="column.dataIndex === 'v4Macs'">
              <div v-if="record.v4Macs?.length" class="flex flex-wrap gap-1">
                <span v-for="m in record.v4Macs" :key="m" class="ipam-chip ipam-mono" data-tone="cyan">{{ m }}</span>
              </div>
              <span v-else class="ipam-hint">无候选</span>
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
              <Button v-if="canSetMapping(record as DualstackConflict)" size="small" @click="openMapping(record as DualstackConflict)">设为映射</Button>
              <Tooltip v-else :title="disableHint(record as DualstackConflict)">
                <span><Button size="small" disabled>设为映射</Button></span>
              </Tooltip>
            </template>
          </template>
        </Table>
      </Card>
    </div>

    <FormModal class="w-[880px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">模板名称<em>*</em></span>
          <Input v-model:value="form.name" placeholder="办公-v4池A" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">IPv4 网段<em>*</em></span>
          <Select v-model:value="form.ipv4Cidr" show-search :options="v4Options" placeholder="选择 IPv4 子网" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">IPv6 前缀<em>*</em></span>
          <Select v-model:value="form.ipv6Prefix" show-search :options="v6Options" placeholder="选择 IPv6 子网" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">地址编码</span>
          <Select v-model:value="form.encoding" :options="(['B', 'A'] as const).map((v) => ({ value: v, label: ENC_TEXT[v] }))" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">生成表达式</span>
          <Select v-model:value="form.expr" :options="EXPR_OPTIONS" placeholder="选择表达式" @change="onExprChange" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">匹配方案</span>
          <Select v-model:value="form.matchScheme" :options="SCHEME_OPTIONS" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">宽限期（小时）</span>
          <InputNumber v-model:value="form.graceHours" :min="0" :max="720" style="width: 100%" />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">DNS 同步</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.dnsSync" size="small" />
            <span class="ipam-hint">{{ form.dnsSync ? '双栈联动时同步 A/AAAA 记录' : '仅生成地址，不写 DNS' }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ EXAMPLE }}；{{ SCHEME_HINT[form.matchScheme] }}
        </div>
      </div>
    </FormModal>

    <MappingModal class="w-[460px]">
      <div class="flex flex-col gap-3">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">v4 MAC 候选</span>
          <Select v-if="mappingMacOptions.length > 1" v-model:value="mappingForm.mac" :options="mappingMacOptions" />
          <Input v-else v-model:value="mappingForm.mac" readonly />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">DUID</span>
          <Input v-model:value="mappingForm.duid" readonly />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">备注</span>
          <Input v-model:value="mappingForm.note" placeholder="可选，说明人工确认依据" />
        </label>
        <div class="ipam-form-hint">映射建立后该终端立即从冲突清单移除，并参与后续双栈关联。</div>
      </div>
    </MappingModal>
  </div>
</template>
