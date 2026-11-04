<script setup lang="ts">
import type { VxeGridProps } from '@vben/plugins/vxe-table';

import type {
  DhcpClassOptionIn,
  DhcpClassRow,
  DhcpOptionRow,
} from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';

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
import {
  createDhcpClass,
  createDhcpOption,
  deleteDhcpClass,
  deleteDhcpOption,
  listDhcpClasses,
  listDhcpOptions,
  updateDhcpClass,
  updateDhcpOption,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const optRows = ref<DhcpOptionRow[]>([]);
const clsRows = ref<DhcpClassRow[]>([]);
const loading = ref(false);
const optForm = ref({ code: 3, name: 'routers', data: '' });
const clsForm = ref({
  name: '',
  test: 'option[61].hex == option[61].hex',
  rows: [] as DhcpClassOptionIn[],
});
const editingOpt = ref<string>();
const editingCls = ref<string>();
const [OptModal, optModalApi] = useVbenModal({
  draggable: true,
  title: $t('dhcp.options.modal.stdTitle'),
  confirmText: $t('dhcp.options.modal.createOption'),
  onConfirm: () => addOption(),
});
const [ClsModal, clsModalApi] = useVbenModal({
  draggable: true,
  title: $t('dhcp.options.classModal.title'),
  confirmText: $t('dhcp.options.classModal.createClass'),
  onConfirm: () => addClass(),
});
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
    message.warning($t('dhcp.options.messages.optRequired'));
    return;
  }
  try {
    if (editingOpt.value) {
      await updateDhcpOption(editingOpt.value, {
        optionCode: optForm.value.code,
        name: optForm.value.name,
        data: optForm.value.data,
      });
      message.success($t('dhcp.options.messages.optUpdated'));
    } else {
      await createDhcpOption({
        optionCode: optForm.value.code,
        name: optForm.value.name,
        data: optForm.value.data,
      });
      message.success($t('dhcp.options.messages.optCreated'));
    }
    editingOpt.value = undefined;
    optModalApi.close();
    optForm.value = { code: optForm.value.code, name: 'routers', data: '' };
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.options.messages.opFailed'),
    );
  }
  await load();
}
async function toggleOption(r: DhcpOptionRow, enabled: boolean) {
  try {
    await updateDhcpOption(r.id, { enabled });
    message.success(
      enabled
        ? $t('dhcp.options.messages.optEnabled')
        : $t('dhcp.options.messages.optDisabled'),
    );
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.options.messages.opFailed'),
    );
  }
  await load();
}
function editOpt(r: DhcpOptionRow) {
  editingOpt.value = r.id;
  optModalApi.setState({
    title: $t('dhcp.options.modal.editTitle'),
    confirmText: $t('dhcp.options.modal.saveEdit'),
  });
  optModalApi.open();
  optForm.value = { code: r.optionCode, name: r.name, data: r.data };
}
function openAddOption() {
  editingOpt.value = undefined;
  optForm.value = { code: 3, name: 'routers', data: '' };
  optModalApi.setState({
    title: $t('dhcp.options.modal.addTitle'),
    confirmText: $t('dhcp.options.modal.createOption'),
  });
  optModalApi.open();
}
async function removeOption(id?: string) {
  if (!id) return;
  try {
    await deleteDhcpOption(id);
    message.success($t('dhcp.options.messages.deleted'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.options.messages.deleteFailed'),
    );
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
    message.warning($t('dhcp.options.messages.clsNameRequired'));
    return;
  }
  if (clsForm.value.rows.some((r) => !r.name || !r.data)) {
    message.warning($t('dhcp.options.messages.clsOptRequired'));
    return;
  }
  try {
    if (editingCls.value) {
      await updateDhcpClass(editingCls.value, {
        test: clsForm.value.test,
        options: clsForm.value.rows,
      });
      message.success($t('dhcp.options.messages.clsUpdated'));
    } else {
      await createDhcpClass({
        name: clsForm.value.name,
        options: clsForm.value.rows,
        test: clsForm.value.test,
      });
      message.success($t('dhcp.options.messages.clsCreated'));
    }
    editingCls.value = undefined;
    clsModalApi.close();
    clsForm.value = { name: '', test: clsForm.value.test, rows: [] };
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.options.messages.opFailed'),
    );
  }
  await load();
}
async function toggleClass(r: DhcpClassRow, enabled: boolean) {
  try {
    await updateDhcpClass(r.id, { enabled });
    message.success(
      enabled
        ? $t('dhcp.options.messages.optEnabled')
        : $t('dhcp.options.messages.optDisabled'),
    );
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.options.messages.opFailed'),
    );
  }
  await load();
}
function editCls(r: DhcpClassRow) {
  editingCls.value = r.id;
  clsModalApi.setState({
    title: $t('dhcp.options.classModal.editTitle'),
    confirmText: $t('dhcp.options.modal.saveEdit'),
  });
  clsModalApi.open();
  clsForm.value = { name: r.name, test: r.test, rows: [...(r.options ?? [])] };
}
function openAddClass() {
  editingCls.value = undefined;
  clsForm.value = {
    name: '',
    test: 'option[61].hex == option[61].hex',
    rows: [],
  };
  clsModalApi.setState({
    title: $t('dhcp.options.classModal.addTitle'),
    confirmText: $t('dhcp.options.classModal.createClass'),
  });
  clsModalApi.open();
}
async function removeClass(id?: string) {
  if (!id) return;
  try {
    await deleteDhcpClass(id);
    message.success($t('dhcp.options.messages.deleted'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.options.messages.deleteFailed'),
    );
  }
  await load();
}
const optsSummary = (options?: DhcpClassOptionIn[]) =>
  (options ?? []).map((o) => `${o.name}=${o.data}`).join(', ') || '-';

// ── 常用选项预设（写库仍走后端校验）──
const PRESETS = computed(() => [
  { code: 3, name: 'routers', hint: $t('dhcp.options.presets.routers') },
  {
    code: 6,
    name: 'domain-name-servers',
    hint: $t('dhcp.options.presets.dnsServers'),
  },
  {
    code: 15,
    name: 'domain-name',
    hint: $t('dhcp.options.presets.domainName'),
  },
  {
    code: 28,
    name: 'broadcast-address',
    hint: $t('dhcp.options.presets.broadcastAddress'),
  },
  {
    code: 42,
    name: 'ntp-servers',
    hint: $t('dhcp.options.presets.ntpServers'),
  },
  { code: 51, name: 'lease-time', hint: $t('dhcp.options.presets.leaseTime') },
  {
    code: 66,
    name: 'tftp-server-name',
    hint: $t('dhcp.options.presets.tftpServer'),
  },
  {
    code: 67,
    name: 'boot-file-name',
    hint: $t('dhcp.options.presets.bootFileName'),
  },
  {
    code: 119,
    name: 'domain-search',
    hint: $t('dhcp.options.presets.domainSearch'),
  },
  {
    code: 121,
    name: 'classless-static-route',
    hint: $t('dhcp.options.presets.classlessRoute'),
  },
  {
    code: 150,
    name: 'tftp-server-address',
    hint: $t('dhcp.options.presets.tftpAddress'),
  },
  { code: 252, name: 'wpad-url', hint: $t('dhcp.options.presets.wpadUrl') },
]);
const presetOptions = computed(() =>
  PRESETS.value.map((p) => ({
    value: p.code,
    label: $t('dhcp.options.presetLabel', {
      code: p.code,
      name: p.name,
      hint: p.hint,
    }),
  })),
);
function applyPreset(code: unknown) {
  const hit = PRESETS.value.find((p) => p.code === code);
  if (!hit) return;
  optForm.value.code = hit.code;
  optForm.value.name = hit.name;
}

// ── 指标 ──
const stats = computed(() => {
  const optEnabled = optRows.value.filter((o) => o.enabled).length;
  const clsEnabled = clsRows.value.filter((c) => c.enabled).length;
  const classOptions = clsRows.value.reduce(
    (n, c) => n + (c.options ?? []).length,
    0,
  );
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
const statusOptions = computed(() => [
  { label: $t('dhcp.options.statusAll'), value: 'all' },
  { label: $t('dhcp.options.statusEnabled'), value: 'enabled' },
  { label: $t('dhcp.options.statusDisabled'), value: 'disabled' },
]);
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
    return `${r.name} ${r.test} ${optsSummary(r.options)}`
      .toLowerCase()
      .includes(kw);
  });
});
function resetClsFilters() {
  clsKeyword.value = '';
  clsStatus.value = 'all';
}

const optGridOptions = computed<VxeGridProps>(() => ({
  columns: [
    {
      field: 'name',
      title: $t('dhcp.options.colName'),
      minWidth: 210,
      slots: { default: 'name' },
    },
    {
      field: 'data',
      title: $t('dhcp.options.colValue'),
      minWidth: 200,
      showOverflow: true,
    },
    {
      field: 'enabled',
      title: $t('dhcp.options.colStatus'),
      width: 90,
      slots: { default: 'enabled' },
    },
    {
      field: 'op',
      title: $t('dhcp.options.colOp'),
      width: 150,
      fixed: 'right',
      slots: { default: 'op' },
    },
  ],
  emptyText: $t('dhcp.options.optEmpty'),
  loading: loading.value,
  rowConfig: { keyField: 'id' },
}));
const [OptGrid, optGridApi] = useVbenVxeGrid({
  gridOptions: optGridOptions.value,
});
// 语言切换后同步表头与空状态文案
watch(optGridOptions, (opts) => optGridApi.setState(opts as any));

const clsGridOptions = computed<VxeGridProps>(() => ({
  columns: [
    { field: 'name', title: $t('dhcp.options.clsColName'), minWidth: 130 },
    {
      field: 'test',
      title: $t('dhcp.options.clsColTest'),
      minWidth: 240,
      slots: { default: 'test' },
    },
    {
      field: 'opts',
      title: $t('dhcp.options.clsColOpts'),
      minWidth: 220,
      slots: { default: 'opts' },
    },
    {
      field: 'enabled',
      title: $t('dhcp.options.colStatus'),
      width: 90,
      slots: { default: 'enabled' },
    },
    {
      field: 'op',
      title: $t('dhcp.options.colOp'),
      width: 150,
      fixed: 'right',
      slots: { default: 'op' },
    },
  ],
  emptyText: $t('dhcp.options.clsEmpty'),
  loading: loading.value,
  rowConfig: { keyField: 'id' },
}));
const [ClsGrid, clsGridApi] = useVbenVxeGrid({
  gridOptions: clsGridOptions.value,
});
watch(clsGridOptions, (opts) => clsGridApi.setState(opts as any));
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
      :title="$t('dhcp.options.title')"
      :description="$t('dhcp.options.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:list-checks" class="size-3.5" />
          {{
            $t('dhcp.options.meta', {
              opts: stats.optTotal,
              classes: stats.clsTotal,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dhcp.common.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openAddOption">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dhcp.options.addOption')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dhcp.options.stat.optTotal')"
        :value="stats.optTotal"
        :unit="$t('dhcp.common.unitCount')"
        icon="lucide:list-checks"
        tone="blue"
        :hint="$t('dhcp.options.stat.optTotalHint', { n: stats.optEnabled })"
      />
      <StatCard
        :label="$t('dhcp.options.stat.optEnabled')"
        :value="stats.optEnabled"
        :unit="$t('dhcp.common.unitCount')"
        icon="lucide:toggle-right"
        tone="green"
        :progress="
          stats.optTotal > 0 ? (stats.optEnabled / stats.optTotal) * 100 : 0
        "
        :hint="
          $t('dhcp.options.stat.optEnabledHint', {
            n: stats.optTotal - stats.optEnabled,
          })
        "
      />
      <StatCard
        :label="$t('dhcp.options.stat.clsTotal')"
        :value="stats.clsTotal"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:filter"
        tone="violet"
        :hint="$t('dhcp.options.stat.clsTotalHint', { n: stats.clsEnabled })"
      />
      <StatCard
        :label="$t('dhcp.options.stat.clsOptions')"
        :value="stats.classOptions"
        :unit="$t('dhcp.common.unitItem')"
        icon="lucide:package-open"
        tone="cyan"
        :hint="
          $t('dhcp.options.stat.clsOptionsHint', {
            avg:
              stats.clsTotal > 0
                ? (stats.classOptions / stats.clsTotal).toFixed(1)
                : '0.0',
          })
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-checks" />
          {{ $t('dhcp.options.standardOptions') }}
          <em class="ipam-card-sub">
            {{ filteredOptions.length }} / {{ optRows.length }}
            {{ $t('dhcp.common.unitCount') }}
          </em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dhcp.options.changesHint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dhcp.options.keyword')
          }}</span>
          <Input
            v-model:value="optKeyword"
            allow-clear
            :placeholder="$t('dhcp.options.optSearchPlaceholder')"
            style="width: 240px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dhcp.options.status') }}</span>
          <Select
            v-model:value="optStatus"
            :options="statusOptions"
            style="width: 130px"
          />
        </div>
        <Button size="small" @click="resetOptFilters">
          {{ $t('dhcp.common.reset') }}
        </Button>
        <Button type="primary" size="small" @click="openAddOption">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dhcp.options.addOption')
          }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dhcp.options.disabledHint') }}</span>
      </div>

      <OptGrid :table-data="filteredOptions">
        <template #name="{ row }">
          <div class="flex items-center gap-2">
            <span class="ipam-code-badge">{{ row.optionCode }}</span>
            <span class="ipam-mono">{{ row.name }}</span>
          </div>
        </template>
        <template #enabled="{ row }">
          <Switch
            :checked="row.enabled"
            size="small"
            @change="(v: any) => toggleOption(row as DhcpOptionRow, !!v)"
          />
        </template>
        <template #op="{ row }">
          <div class="flex items-center gap-1">
            <Button size="small" @click="editOpt(row as DhcpOptionRow)">
              {{ $t('dhcp.common.edit') }}
            </Button>
            <Popconfirm
              :title="$t('dhcp.options.confirmDeleteOpt')"
              :description="$t('dhcp.options.deleteDesc')"
              :ok-text="$t('dhcp.common.delete')"
              :cancel-text="$t('dhcp.common.cancel')"
              @confirm="removeOption(row.id)"
            >
              <Button size="small" danger>
                {{ $t('dhcp.common.delete') }}
              </Button>
            </Popconfirm>
          </div>
        </template>
      </OptGrid>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:filter" />
          {{ $t('dhcp.options.cls') }}
          <em class="ipam-card-sub">
            {{ filteredClasses.length }} / {{ clsRows.length }}
            {{ $t('dhcp.common.unitRecord') }}
          </em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dhcp.options.clsExtraHint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dhcp.options.keyword')
          }}</span>
          <Input
            v-model:value="clsKeyword"
            allow-clear
            :placeholder="$t('dhcp.options.clsSearchPlaceholder')"
            style="width: 260px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dhcp.options.status') }}</span>
          <Select
            v-model:value="clsStatus"
            :options="statusOptions"
            style="width: 130px"
          />
        </div>
        <Button size="small" @click="resetClsFilters">
          {{ $t('dhcp.common.reset') }}
        </Button>
        <Button type="primary" size="small" @click="openAddClass">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dhcp.options.addClass')
          }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dhcp.options.clsHint') }}</span>
      </div>

      <ClsGrid :table-data="filteredClasses">
        <template #opts="{ row }">
          <span class="ipam-mono text-xs">{{ optsSummary(row.options) }}</span>
        </template>
        <template #test="{ row }">
          <span class="ipam-chip ipam-mono" data-tone="violet">{{
            row.test || '-'
          }}</span>
        </template>
        <template #enabled="{ row }">
          <Switch
            :checked="row.enabled"
            size="small"
            @change="(v: any) => toggleClass(row as DhcpClassRow, !!v)"
          />
        </template>
        <template #op="{ row }">
          <div class="flex items-center gap-1">
            <Button size="small" @click="editCls(row as DhcpClassRow)">
              {{ $t('dhcp.common.edit') }}
            </Button>
            <Popconfirm
              :title="$t('dhcp.options.confirmDeleteCls')"
              :description="$t('dhcp.options.deleteDesc')"
              :ok-text="$t('dhcp.common.delete')"
              :cancel-text="$t('dhcp.common.cancel')"
              @confirm="removeClass(row.id)"
            >
              <Button size="small" danger>
                {{ $t('dhcp.common.delete') }}
              </Button>
            </Popconfirm>
          </div>
        </template>
      </ClsGrid>
    </Card>

    <OptModal class="w-[760px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dhcp.options.modal.quickFill')
          }}</span>
          <Select
            :options="presetOptions"
            allow-clear
            show-search
            :placeholder="$t('dhcp.options.modal.quickFillPlaceholder')"
            @change="applyPreset"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dhcp.options.modal.code') }}<em>*</em></span
          >
          <InputNumber
            v-model:value="optForm.code"
            :min="1"
            :max="255"
            style="width: 100%"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dhcp.options.modal.keaName') }}<em>*</em></span
          >
          <Input v-model:value="optForm.name" placeholder="routers" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dhcp.options.modal.value') }}<em>*</em></span
          >
          <Input
            v-model:value="optForm.data"
            :placeholder="$t('dhcp.options.modal.valuePlaceholder')"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dhcp.options.modal.hint') }}
        </div>
      </div>
    </OptModal>

    <ClsModal class="w-[820px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dhcp.options.classModal.className') }}<em>*</em></span
          >
          <Input
            v-model:value="clsForm.name"
            :disabled="!!editingCls"
            placeholder="printers"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dhcp.options.classModal.testExpr') }}<em>*</em></span
          >
          <Input
            v-model:value="clsForm.test"
            placeholder="option[61].hex == option[61].hex"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dhcp.options.classModal.hint') }}
        </div>
        <div class="ipam-form-field--wide">
          <div class="mb-2 flex items-center justify-between">
            <span class="ipam-form-field__label">
              {{ $t('dhcp.options.classModal.optionsInClass') }}
            </span>
            <Button size="small" @click="addClassRow">
              <IconifyIcon icon="lucide:plus" class="mr-1" />{{
                $t('dhcp.options.addOption')
              }}
            </Button>
          </div>
          <div
            v-if="clsForm.rows.length === 0"
            class="ipam-empty ipam-empty--compact"
          >
            <div class="ipam-empty__title">
              {{ $t('dhcp.options.classModal.emptyTitle') }}
            </div>
            <div class="ipam-empty__desc">
              {{ $t('dhcp.options.classModal.emptyDesc') }}
            </div>
          </div>
          <div v-for="(r, i) in clsForm.rows" :key="i" class="ipam-class-row">
            <InputNumber
              v-model:value="r.optionCode"
              :min="1"
              :max="255"
              style="width: 90px"
            />
            <Input
              v-model:value="r.name"
              style="width: 180px"
              placeholder="routers"
            />
            <Input
              v-model:value="r.data"
              style="flex: 1; min-width: 200px"
              placeholder="192.168.9.254"
            />
            <Button size="small" danger @click="removeClassRow(i)">
              {{ $t('dhcp.options.classModal.remove') }}
            </Button>
          </div>
        </div>
      </div>
    </ClsModal>
  </div>
</template>
