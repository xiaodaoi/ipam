<script setup lang="ts">
import type { VxeGridProps } from '@vben/plugins/vxe-table';

import type { DhcpClassOptionIn, DhcpClassRow, DhcpOptionRow } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Switch,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { createDhcpClass, createDhcpOption, deleteDhcpClass, deleteDhcpOption, listDhcpClasses, listDhcpOptions, updateDhcpClass, updateDhcpOption } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const optRows = ref<DhcpOptionRow[]>([]);
const clsRows = ref<DhcpClassRow[]>([]);
const loading = ref(false);
const optForm = ref({ code: 3, name: 'routers', data: '' });
const clsForm = ref({ name: '', test: "option[61].hex == option[61].hex", rows: [] as DhcpClassOptionIn[] });
const editingOpt = ref<string>();
const editingCls = ref<string>();
const [OptModal, optModalApi] = useVbenModal({ draggable: true, title: '标准选项', confirmText: '创建选项', onConfirm: () => addOption() });
const [ClsModal, clsModalApi] = useVbenModal({ draggable: true, title: '类匹配规则', confirmText: '创建类', onConfirm: () => addClass() });
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
  loading.value = true;
  try {
    const [o, c] = await Promise.all([listDhcpOptions(), listDhcpClasses()]);
    optRows.value = o.items ?? [];
    clsRows.value = c.items ?? [];
  } finally {
    loading.value = false;
  }
}

async function addOption() {
  if (!optForm.value.name || !optForm.value.data) {
    message.warning('选项名与值必填');
    return;
  }
  try {
    if (editingOpt.value) {
      await updateDhcpOption(editingOpt.value, {
        optionCode: optForm.value.code, name: optForm.value.name, data: optForm.value.data,
      });
      message.success('选项已更新');
    } else {
      await createDhcpOption({
        optionCode: optForm.value.code, name: optForm.value.name, data: optForm.value.data,
      });
      message.success('选项已创建');
    }
    editingOpt.value = undefined;
    optModalApi.close();
    optForm.value = { code: optForm.value.code, name: 'routers', data: '' };
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
async function toggleOption(r: DhcpOptionRow, enabled: boolean) {
  try {
    await updateDhcpOption(r.id, { enabled });
    message.success(enabled ? '已启用' : '已停用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
function editOpt(r: DhcpOptionRow) {
  editingOpt.value = r.id;
  optModalApi.setState({ title: '编辑选项', confirmText: '保存修改' });
  optModalApi.open();
  optForm.value = { code: r.optionCode, name: r.name, data: r.data };
}
function openAddOption() {
  editingOpt.value = undefined;
  optForm.value = { code: 3, name: 'routers', data: '' };
  optModalApi.setState({ title: '添加标准选项', confirmText: '创建选项' });
  optModalApi.open();
}
async function removeOption(id?: string) {
  if (!id) return;
  try {
    await deleteDhcpOption(id);
    message.success('已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}

function addClassRow() {
  clsForm.value.rows.push({ optionCode: 3, name: 'routers', data: '' });
}
function removeClassRow(i: number) {
  clsForm.value.rows.splice(i, 1);
}
async function addClass() {
  if (!clsForm.value.name) {
    message.warning('类名必填');
    return;
  }
  if (clsForm.value.rows.some((r) => !r.name || !r.data)) {
    message.warning('类内选项的名称与值必填');
    return;
  }
  try {
    if (editingCls.value) {
      await updateDhcpClass(editingCls.value, { test: clsForm.value.test, options: clsForm.value.rows });
      message.success('类已更新');
    } else {
      await createDhcpClass({
        name: clsForm.value.name,
        options: clsForm.value.rows,
        test: clsForm.value.test,
      });
      message.success('类已创建');
    }
    editingCls.value = undefined;
    clsModalApi.close();
    clsForm.value = { name: '', test: clsForm.value.test, rows: [] };
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
async function toggleClass(r: DhcpClassRow, enabled: boolean) {
  try {
    await updateDhcpClass(r.id, { enabled });
    message.success(enabled ? '已启用' : '已停用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
function editCls(r: DhcpClassRow) {
  editingCls.value = r.id;
  clsModalApi.setState({ title: '编辑类', confirmText: '保存修改' });
  clsModalApi.open();
  clsForm.value = { name: r.name, test: r.test, rows: [...(r.options ?? [])] };
}
function openAddClass() {
  editingCls.value = undefined;
  clsForm.value = { name: '', test: "option[61].hex == option[61].hex", rows: [] };
  clsModalApi.setState({ title: '添加类匹配规则', confirmText: '创建类' });
  clsModalApi.open();
}
async function removeClass(id?: string) {
  if (!id) return;
  try {
    await deleteDhcpClass(id);
    message.success('已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}
const optsSummary = (options?: DhcpClassOptionIn[]) =>
  (options ?? []).map((o) => `${o.name}=${o.data}`).join(', ') || '-';

// ── 常用选项预设（写库仍走后端校验）──
const PRESETS = [
  { code: 3, name: 'routers', hint: '默认网关' },
  { code: 6, name: 'domain-name-servers', hint: 'DNS 服务器' },
  { code: 15, name: 'domain-name', hint: '默认域名' },
  { code: 28, name: 'broadcast-address', hint: '广播地址' },
  { code: 42, name: 'ntp-servers', hint: 'NTP 服务器' },
  { code: 51, name: 'lease-time', hint: '租约时长' },
  { code: 66, name: 'tftp-server-name', hint: 'TFTP 服务器' },
  { code: 67, name: 'boot-file-name', hint: '启动文件名' },
  { code: 119, name: 'domain-search', hint: '域搜索列表' },
  { code: 121, name: 'classless-static-route', hint: '无类静态路由' },
  { code: 150, name: 'tftp-server-address', hint: 'TFTP 地址' },
  { code: 252, name: 'wpad-url', hint: '代理自动发现' },
];
const presetOptions = PRESETS.map((p) => ({
  value: p.code,
  label: `${p.code} · ${p.name}（${p.hint}）`,
}));
function applyPreset(code: unknown) {
  const hit = PRESETS.find((p) => p.code === code);
  if (!hit) return;
  optForm.value.code = hit.code;
  optForm.value.name = hit.name;
}

// ── 指标 ──
const stats = computed(() => {
  const optEnabled = optRows.value.filter((o) => o.enabled).length;
  const clsEnabled = clsRows.value.filter((c) => c.enabled).length;
  const classOptions = clsRows.value.reduce((n, c) => n + (c.options ?? []).length, 0);
  return {
    clsEnabled,
    classOptions,
    clsTotal: clsRows.value.length,
    optEnabled,
    optTotal: optRows.value.length,
  };
});

// ── 筛选 ──
const optKeyword = ref('');
const optStatus = ref<'all' | 'disabled' | 'enabled'>('all');
const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '已启用', value: 'enabled' },
  { label: '已停用', value: 'disabled' },
];
const filteredOptions = computed(() => {
  const kw = optKeyword.value.trim().toLowerCase();
  return optRows.value.filter((r) => {
    if (optStatus.value === 'enabled' && !r.enabled) return false;
    if (optStatus.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    return `${r.optionCode} ${r.name} ${r.data}`.toLowerCase().includes(kw);
  });
});
function resetOptFilters() {
  optKeyword.value = '';
  optStatus.value = 'all';
}

const clsKeyword = ref('');
const clsStatus = ref<'all' | 'disabled' | 'enabled'>('all');
const filteredClasses = computed(() => {
  const kw = clsKeyword.value.trim().toLowerCase();
  return clsRows.value.filter((r) => {
    if (clsStatus.value === 'enabled' && !r.enabled) return false;
    if (clsStatus.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    return `${r.name} ${r.test} ${optsSummary(r.options)}`.toLowerCase().includes(kw);
  });
});
function resetClsFilters() {
  clsKeyword.value = '';
  clsStatus.value = 'all';
}

const optGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '选项（码 / 名称）', minWidth: 210, slots: { default: 'name' } },
    { field: 'data', title: '值', minWidth: 200, showOverflow: true },
    { field: 'enabled', title: '状态', width: 90, slots: { default: 'enabled' } },
    { field: 'op', title: '操作', width: 150, fixed: 'right', slots: { default: 'op' } },
  ],
  emptyText: '暂无标准选项，点击「添加选项」创建',
  loading: loading.value,
  rowConfig: { keyField: 'id' },
});
const [OptGrid] = useVbenVxeGrid({ gridOptions: optGridOptions });

const clsGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '类名', minWidth: 130 },
    { field: 'test', title: 'test 表达式', minWidth: 240, slots: { default: 'test' } },
    { field: 'opts', title: '下发选项', minWidth: 220, slots: { default: 'opts' } },
    { field: 'enabled', title: '状态', width: 90, slots: { default: 'enabled' } },
    { field: 'op', title: '操作', width: 150, fixed: 'right', slots: { default: 'op' } },
  ],
  emptyText: '暂无类匹配规则，点击「添加类」创建',
  loading: loading.value,
  rowConfig: { keyField: 'id' },
});
const [ClsGrid] = useVbenVxeGrid({ gridOptions: clsGridOptions });
onMounted(() => {
  void load();
  timer = setInterval(load, 15_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:sliders-horizontal"
      title="选项与类匹配"
      description="维护全局 DHCP option-data 与 client-classes 匹配规则，变更后原子下发 Kea。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:list-checks" class="size-3.5" />
          {{ stats.optTotal }} 个选项 · {{ stats.clsTotal }} 条类规则
        </span>
        <Button size="small" :loading="loading" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openAddOption">
          <IconifyIcon icon="lucide:plus" class="mr-1" />添加选项
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="标准选项"
        :value="stats.optTotal"
        unit="个"
        icon="lucide:list-checks"
        tone="blue"
        :hint="`${stats.optEnabled} 个已启用`"
      />
      <StatCard
        label="启用选项"
        :value="stats.optEnabled"
        unit="个"
        icon="lucide:toggle-right"
        tone="green"
        :progress="stats.optTotal > 0 ? (stats.optEnabled / stats.optTotal) * 100 : 0"
        :hint="`停用 ${stats.optTotal - stats.optEnabled} 个不进入 Kea 配置`"
      />
      <StatCard
        label="类匹配规则"
        :value="stats.clsTotal"
        unit="条"
        icon="lucide:filter"
        tone="violet"
        :hint="`${stats.clsEnabled} 条已启用`"
      />
      <StatCard
        label="类内下发选项"
        :value="stats.classOptions"
        unit="项"
        icon="lucide:package-open"
        tone="cyan"
        :hint="`平均每条类 ${stats.clsTotal > 0 ? (stats.classOptions / stats.clsTotal).toFixed(1) : '0.0'} 项`"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-checks" />
          标准选项
          <em class="ipam-card-sub">{{ filteredOptions.length }} / {{ optRows.length }} 个</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">变更后经 Kea config-set 原子下发</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input v-model:value="optKeyword" allow-clear placeholder="选项码 / 名称 / 值" style="width: 240px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">状态</span>
          <Select v-model:value="optStatus" :options="statusOptions" style="width: 130px" />
        </div>
        <Button size="small" @click="resetOptFilters">重置</Button>
        <Button type="primary" size="small" @click="openAddOption">
          <IconifyIcon icon="lucide:plus" class="mr-1" />添加选项
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">disabled 选项不会进入 Kea 配置</span>
      </div>

      <OptGrid :table-data="filteredOptions">
        <template #name="{ row }">
          <div class="flex items-center gap-2">
            <span class="ipam-code-badge">{{ row.optionCode }}</span>
            <span class="ipam-mono">{{ row.name }}</span>
          </div>
        </template>
        <template #enabled="{ row }">
          <Switch :checked="row.enabled" size="small" @change="(v: any) => toggleOption(row as DhcpOptionRow, !!v)" />
        </template>
        <template #op="{ row }">
          <div class="flex items-center gap-1">
            <Button size="small" @click="editOpt(row as DhcpOptionRow)">编辑</Button>
            <Popconfirm title="确认删除该选项？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="removeOption(row.id)">
              <Button size="small" danger>删除</Button>
            </Popconfirm>
          </div>
        </template>
      </OptGrid>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:filter" />
          类匹配规则
          <em class="ipam-card-sub">{{ filteredClasses.length }} / {{ clsRows.length }} 条</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">client-classes · test 命中后下发类内选项</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input v-model:value="clsKeyword" allow-clear placeholder="类名 / test / 下发选项" style="width: 260px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">状态</span>
          <Select v-model:value="clsStatus" :options="statusOptions" style="width: 130px" />
        </div>
        <Button size="small" @click="resetClsFilters">重置</Button>
        <Button type="primary" size="small" @click="openAddClass">
          <IconifyIcon icon="lucide:plus" class="mr-1" />添加类
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">类名是 Kea 引用键，创建后不可修改</span>
      </div>

      <ClsGrid :table-data="filteredClasses">
        <template #opts="{ row }">
          <span class="ipam-mono text-xs">{{ optsSummary(row.options) }}</span>
        </template>
        <template #test="{ row }">
          <span class="ipam-chip ipam-mono" data-tone="violet">{{ row.test || '-' }}</span>
        </template>
        <template #enabled="{ row }">
          <Switch :checked="row.enabled" size="small" @change="(v: any) => toggleClass(row as DhcpClassRow, !!v)" />
        </template>
        <template #op="{ row }">
          <div class="flex items-center gap-1">
            <Button size="small" @click="editCls(row as DhcpClassRow)">编辑</Button>
            <Popconfirm title="确认删除该分类？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="removeClass(row.id)">
              <Button size="small" danger>删除</Button>
            </Popconfirm>
          </div>
        </template>
      </ClsGrid>
    </Card>

    <OptModal class="w-[760px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">常用选项（快速填充）</span>
          <Select
            :options="presetOptions"
            allow-clear
            show-search
            placeholder="选择后自动填入选项码与 Kea 名称"
            @change="applyPreset"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">选项码<em>*</em></span>
          <InputNumber v-model:value="optForm.code" :min="1" :max="255" style="width: 100%" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">Kea 选项名<em>*</em></span>
          <Input v-model:value="optForm.name" placeholder="routers" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">选项值<em>*</em></span>
          <Input v-model:value="optForm.data" placeholder="如 192.168.9.1 / 10.0.0.10,10.0.0.11" />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          保存后经 Kea config-set 原子下发；停用的选项不会写入配置文件。
        </div>
      </div>
    </OptModal>

    <ClsModal class="w-[820px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">类名<em>*</em></span>
          <Input v-model:value="clsForm.name" :disabled="!!editingCls" placeholder="printers" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">test 表达式（Kea eval）<em>*</em></span>
          <Input v-model:value="clsForm.test" placeholder="option[61].hex == option[61].hex" />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          命中 test 的客户端将收到该类下的 option-data；类名为 Kea 引用键，创建后不可改。
        </div>
        <div class="ipam-form-field--wide">
          <div class="mb-2 flex items-center justify-between">
            <span class="ipam-form-field__label">类内下发选项</span>
            <Button size="small" @click="addClassRow">
              <IconifyIcon icon="lucide:plus" class="mr-1" />添加选项
            </Button>
          </div>
          <div v-if="clsForm.rows.length === 0" class="ipam-empty ipam-empty--compact">
            <div class="ipam-empty__title">尚未添加类内选项</div>
            <div class="ipam-empty__desc">点击「添加选项」为该类配置命中的客户端将收到的 option-data</div>
          </div>
          <div v-for="(r, i) in clsForm.rows" :key="i" class="ipam-class-row">
            <InputNumber v-model:value="r.optionCode" :min="1" :max="255" style="width: 90px" />
            <Input v-model:value="r.name" style="width: 180px" placeholder="routers" />
            <Input v-model:value="r.data" style="flex: 1; min-width: 200px" placeholder="192.168.9.254" />
            <Button size="small" danger @click="removeClassRow(i)">移除</Button>
          </div>
        </div>
      </div>
    </ClsModal>
  </div>
</template>
