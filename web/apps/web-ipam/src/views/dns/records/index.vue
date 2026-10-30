<script setup lang="ts">
import type { DnsRecord, DnsZone } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Modal,
  Popconfirm,
  RadioGroup,
  Select,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import { createDnsRecord, createDnsZone, deleteDnsRecord, deleteDnsZone, importDnsHosts, listDnsRecords, listDnsZones, listLinkedRecords, updateDnsRecord, updateDnsZone } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

type RecType = 'A' | 'AAAA' | 'CNAME' | 'MX' | 'PTR' | 'SRV' | 'TXT';

const showTotal = (t: number) => `共 ${t} 条`;
const REC_TYPES: RecType[] = ['A', 'AAAA', 'CNAME', 'PTR', 'TXT', 'SRV', 'MX'];

const zones = ref<DnsZone[]>([]);
const zoneId = ref<string>();
const records = ref<DnsRecord[]>([]);
const linked = ref<any[]>([]);
const loading = ref(false);

const activeZone = computed(() => zones.value.find((z) => z.id === zoneId.value));

// 完整域名：记录名 + 区域（名称已含区域后缀则只补尾点）
function fqdnOf(name: string): string {
  const z = activeZone.value?.name;
  if (!z) return name;
  if (name.endsWith('.')) return name;
  const root = z.replace(/\.$/, '');
  if (name === root || name.endsWith(`.${root}`)) return `${name}.`;
  return `${name}.${z}`;
}

async function loadZones() {
  const d = await listDnsZones();
  zones.value = d.items ?? [];
  if (!zoneId.value || !zones.value.some((z) => z.id === zoneId.value)) {
    zoneId.value = zones.value[0]?.id;
  }
  if (zoneId.value) await loadRecords();
}

async function loadRecords() {
  if (!zoneId.value) {
    records.value = [];
    linked.value = [];
    return;
  }
  loading.value = true;
  try {
    const [rec, linkedRes] = await Promise.all([
      listDnsRecords(zoneId.value),
      listLinkedRecords(zoneId.value),
    ]);
    records.value = rec.items ?? [];
    linked.value = linkedRes.items ?? [];
  } finally {
    loading.value = false;
  }
}

function selectZone(id: string) {
  zoneId.value = id;
  recTab.value = 'rec';
  void loadRecords();
}

const recTab = ref<'linked' | 'rec'>('rec');

// ── 新增记录表单 ──
const recForm = reactive<{ name: string; rdata: string; recType: RecType; ttl: number }>({
  name: '', recType: 'A', rdata: '', ttl: 300,
});
const [AddRecordModal, addRecordModalApi] = useVbenModal({
  draggable: true,
  title: '新增解析记录',
  confirmText: '创建记录',
  onConfirm: () => addRecord(),
});

function openAddRecord() {
  if (!zoneId.value) {
    message.warning('请先选择或新建区域');
    return;
  }
  recForm.name = '';
  recForm.recType = 'A';
  recForm.rdata = '';
  recForm.ttl = 300;
  addRecordModalApi.setState({ title: `新增记录 · ${activeZone.value?.name ?? ''}` });
  addRecordModalApi.open();
}

async function addRecord() {
  if (!zoneId.value) return;
  if (!recForm.name || !recForm.rdata) {
    message.warning('请填写记录名称与记录值');
    return;
  }
  try {
    await createDnsRecord(zoneId.value, { ...recForm, enabled: true });
    message.success('记录已创建');
    addRecordModalApi.close();
    recForm.name = '';
    recForm.rdata = '';
    await loadRecords();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '添加失败');
  }
}

// ── 编辑记录 ──
const editRecord = ref<DnsRecord>();
const recordEdit = reactive<{ enabled: boolean; name: string; rdata: string; recType: RecType; ttl: number }>({
  name: '', recType: 'A', rdata: '', ttl: 300, enabled: true,
});
const [RecordModal, recordModalApi] = useVbenModal({
  draggable: true,
  confirmText: '保存',
  onConfirm: () => confirmEditRecord(),
});

function openEditRecord(r: DnsRecord) {
  editRecord.value = r;
  Object.assign(recordEdit, {
    name: r.name, recType: r.recType, rdata: r.rdata, ttl: r.ttl, enabled: r.enabled,
  });
  recordModalApi.setState({ title: `编辑记录 · ${r.name}` });
  recordModalApi.open();
}

async function confirmEditRecord() {
  const r = editRecord.value;
  if (!r || !zoneId.value) return;
  if (!recordEdit.name || !recordEdit.rdata) {
    message.warning('名称与值必填');
    return;
  }
  try {
    await updateDnsRecord(zoneId.value, r.id, { ...recordEdit });
    message.success('记录已更新');
    recordModalApi.close();
    await loadRecords();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '更新失败');
  }
}

async function toggleRecord(r: DnsRecord, enabled: boolean) {
  if (!zoneId.value) return;
  try {
    await updateDnsRecord(zoneId.value, r.id, { enabled });
    await loadRecords();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
}

function removeRecord(r: DnsRecord) {
  Modal.confirm({
    title: `删除记录 ${r.name}`,
    content: `将删除 ${activeZone.value?.name ?? ''} 下的 ${r.name} ${r.recType} 记录。`,
    okText: '删除',
    okType: 'danger',
    onOk: async () => {
      if (!zoneId.value) return;
      try {
        await deleteDnsRecord(zoneId.value, r.id);
        message.success('记录已删除');
        await loadRecords();
      } catch (error) {
        message.error(error instanceof Error ? error.message : '删除失败');
      }
    },
  });
}

// ── 新建/编辑 zone ──
const zoneForm = reactive<{ kind: 'auth' | 'local'; name: string }>({ name: '', kind: 'auth' });
const editingZoneId = ref<string>();
const [ZoneModal, zoneModalApi] = useVbenModal({
  draggable: true,
  confirmText: '保存',
  onConfirm: () => confirmZone(),
});

function openCreateZone() {
  editingZoneId.value = undefined;
  zoneForm.name = '';
  zoneForm.kind = 'auth';
  zoneModalApi.setState({ title: '新建 DNS 区域', confirmText: '创建区域' });
  zoneModalApi.open();
}

function openEditZone(z: DnsZone) {
  editingZoneId.value = z.id;
  zoneForm.name = z.name.replace(/\.$/, '');
  zoneForm.kind = z.kind;
  zoneModalApi.setState({ title: `编辑区域 · ${z.name}`, confirmText: '保存修改' });
  zoneModalApi.open();
}

async function confirmZone() {
  const name = zoneForm.name.trim();
  if (!name) return;
  const fqdn = name.endsWith('.') ? name : `${name}.`;
  try {
    if (editingZoneId.value) {
      await updateDnsZone(editingZoneId.value, { name: fqdn, kind: zoneForm.kind });
      message.success('区域已更新');
    } else {
      await createDnsZone({ name: fqdn, kind: zoneForm.kind });
      message.success('区域已创建');
    }
    zoneModalApi.close();
    await loadZones();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  }
}

async function toggleZone(z: DnsZone, enabled: boolean) {
  try {
    await updateDnsZone(z.id, { enabled });
    await loadZones();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
}

function removeZone(z: DnsZone) {
  Modal.confirm({
    title: `删除区域 ${z.name}`,
    content: '其下全部解析记录将一并删除，且从 resolver 配置移除。',
    okText: '删除',
    okType: 'danger',
    onOk: async () => {
      try {
        await deleteDnsZone(z.id);
        if (zoneId.value === z.id) zoneId.value = undefined;
        message.success('区域已删除');
        await loadZones();
      } catch (error) {
        message.error(error instanceof Error ? error.message : '删除失败');
      }
    },
  });
}

onMounted(loadZones);

// ── 指标 ──
const stats = computed(() => {
  const zoneTotal = zones.value.length;
  const zoneEnabled = zones.value.filter((z) => z.enabled).length;
  const disabled = records.value.filter((r) => !r.enabled).length;
  const typeCount = new Map<string, number>();
  for (const r of records.value) typeCount.set(r.recType, (typeCount.get(r.recType) ?? 0) + 1);
  return {
    disabled,
    enabledRecords: records.value.length - disabled,
    linked: linked.value.length,
    records: records.value.length,
    typeCount,
    zoneEnabled,
    zoneTotal,
  };
});

// ── 区域筛选 ──
const zoneKeyword = ref('');
const zoneStatus = ref<'all' | 'disabled' | 'enabled'>('all');
const filteredZones = computed(() => {
  const kw = zoneKeyword.value.trim().toLowerCase();
  return zones.value.filter((z) => {
    if (zoneStatus.value === 'enabled' && !z.enabled) return false;
    if (zoneStatus.value === 'disabled' && z.enabled) return false;
    return !kw || z.name.toLowerCase().includes(kw);
  });
});

// ── 记录筛选 ──
const recKeyword = ref('');
const hostsOpen = ref(false);
const hostsContent = ref('');
const importing = ref(false);

function openHostsImport() {
  if (!zoneId.value) {
    message.warning('请先选择区域');
    return;
  }
  hostsContent.value = '';
  hostsOpen.value = true;
}

async function submitHostsImport() {
  if (!zoneId.value || !hostsContent.value.trim()) {
    message.warning('请粘贴 hosts 内容');
    return;
  }
  importing.value = true;
  try {
    const r = await importDnsHosts(zoneId.value, hostsContent.value);
    message.success(`导入完成：新增 ${r.created}，跳过 ${r.skipped}`);
    if (r.errors?.length) message.warning(r.errors.slice(0, 3).join('；'));
    hostsOpen.value = false;
    await loadRecords();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导入失败');
  } finally {
    importing.value = false;
  }
}

const recTypeFilter = ref<'all' | RecType>('all');
const recStatus = ref<'all' | 'disabled' | 'enabled'>('all');
const typeOptions = [{ label: '全部类型', value: 'all' }, ...REC_TYPES.map((t) => ({ label: t, value: t }))];
const recFiltered = computed(() => {
  const kw = recKeyword.value.trim().toLowerCase();
  return records.value.filter((r) => {
    if (recTypeFilter.value !== 'all' && r.recType !== recTypeFilter.value) return false;
    if (recStatus.value === 'enabled' && !r.enabled) return false;
    if (recStatus.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    return `${r.name} ${fqdnOf(r.name)} ${r.rdata}`.toLowerCase().includes(kw);
  });
});
const linkedKeyword = ref('');
const linkedFiltered = computed(() => {
  const kw = linkedKeyword.value.trim().toLowerCase();
  if (!kw) return linked.value;
  return linked.value.filter((r) =>
    `${r.name ?? ''} ${r.rdata ?? ''} ${r.mac ?? ''}`.toLowerCase().includes(kw),
  );
});
function resetRecFilters() {
  recKeyword.value = '';
  recTypeFilter.value = 'all';
  recStatus.value = 'all';
}
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:book-open"
      title="解析记录"
      description="按区域维护 A / AAAA / CNAME / PTR / TXT / SRV / MX 记录，并查看 DHCP 双栈联动自动生成的记录。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layers" class="size-3.5" />
          {{ stats.zoneTotal }} 个区域 · {{ stats.records }} 条记录
        </span>
        <Button size="small" :loading="loading" @click="loadZones()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button size="small" @click="openCreateZone">
          <IconifyIcon icon="lucide:folder-plus" class="mr-1" />新建区域
        </Button>
        <Button size="small" @click="openHostsImport">
          <IconifyIcon icon="lucide:upload" class="mr-1" />导入 hosts
        </Button>
        <Button type="primary" size="small" @click="openAddRecord">
          <IconifyIcon icon="lucide:plus" class="mr-1" />新增记录
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="DNS 区域"
        :value="stats.zoneTotal"
        unit="个"
        icon="lucide:layers"
        tone="blue"
        :hint="`${stats.zoneEnabled} 个启用 · ${stats.zoneTotal - stats.zoneEnabled} 个停用`"
      />
      <StatCard
        label="当前区域记录"
        :value="stats.records"
        unit="条"
        icon="lucide:book-open"
        tone="cyan"
        :hint="activeZone ? `${activeZone.name} · 启用 ${stats.enabledRecords} 条` : '请选择区域'"
      />
      <StatCard
        label="联动记录"
        :value="stats.linked"
        unit="条"
        icon="lucide:git-compare-arrows"
        tone="violet"
        :hint="stats.linked > 0 ? '由 DHCP 双栈联动自动派生' : '暂无联动记录'"
      />
      <StatCard
        label="停用记录"
        :value="stats.disabled"
        unit="条"
        icon="lucide:eye-off"
        :tone="stats.disabled > 0 ? 'amber' : 'green'"
        :hint="stats.records > 0 ? `启用率 ${Math.round((stats.enabledRecords / stats.records) * 100)}%` : '当前区域暂无记录'"
      />
    </div>

    <div class="ipam-record-layout">
      <!-- 左侧 zone 列表 -->
      <Card class="ipam-side-card ipam-side-card--wide">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:layers" />
            DNS 区域
            <em class="ipam-card-sub">{{ filteredZones.length }} / {{ zones.length }}</em>
          </span>
        </template>

        <div class="mb-2 flex flex-col gap-2">
          <Input v-model:value="zoneKeyword" allow-clear placeholder="搜索区域" size="small" />
          <Select
            v-model:value="zoneStatus"
            size="small"
            :options="[
              { label: '全部状态', value: 'all' },
              { label: '启用中', value: 'enabled' },
              { label: '已停用', value: 'disabled' },
            ]"
          />
        </div>

        <div v-if="filteredZones.length === 0" class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">{{ zones.length === 0 ? '暂无 DNS 区域' : '没有匹配的区域' }}</div>
          <div class="ipam-empty__desc">{{ zones.length === 0 ? '点击右上角「新建区域」创建第一个权威或本地区域' : '调整搜索关键字或状态筛选' }}</div>
        </div>

        <div
          v-for="z in filteredZones"
          :key="z.id"
          class="ipam-zone-item"
          :class="zoneId === z.id ? 'ipam-zone-item--active' : ''"
          @click="selectZone(z.id)"
        >
          <div class="flex items-center justify-between gap-1">
            <span class="truncate text-sm font-medium" :title="z.name">{{ z.name }}</span>
            <Tag :color="z.kind === 'auth' ? 'blue' : 'purple'" class="m-0">{{ z.kind }}</Tag>
          </div>
          <div class="mt-1 flex items-center justify-between">
            <span class="ipam-hint">{{ z.enabled ? '启用中' : '已停用' }}</span>
            <div class="flex items-center gap-1" @click.stop>
              <Switch :checked="z.enabled" size="small" @change="(v: any) => toggleZone(z as DnsZone, !!v)" />
              <Button size="small" type="text" @click="openEditZone(z)">编辑</Button>
              <Button size="small" type="text" danger @click="removeZone(z)">删除</Button>
            </div>
          </div>
        </div>
      </Card>

      <!-- 右侧记录 -->
      <Card class="min-w-0 flex-1">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:list-tree" />
            解析记录
            <em class="ipam-card-sub">{{ activeZone ? activeZone.name : '未选择区域' }}</em>
          </span>
        </template>

        <div v-if="activeZone">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
            <RadioGroup
              v-model:value="recTab"
              option-type="button"
              size="small"
              :options="[
                { label: `静态记录（${records.length}）`, value: 'rec' },
                { label: `联动记录（${linked.length}）`, value: 'linked' },
              ]"
            />
            <div class="flex flex-wrap items-center gap-2">
              <span class="ipam-chip" data-tone="muted">A {{ stats.typeCount.get('A') ?? 0 }}</span>
              <span class="ipam-chip" data-tone="muted">AAAA {{ stats.typeCount.get('AAAA') ?? 0 }}</span>
              <span class="ipam-chip" data-tone="muted">CNAME {{ stats.typeCount.get('CNAME') ?? 0 }}</span>
              <span class="ipam-chip" data-tone="muted">PTR {{ stats.typeCount.get('PTR') ?? 0 }}</span>
              <span class="ipam-chip" data-tone="muted">TXT {{ stats.typeCount.get('TXT') ?? 0 }}</span>
              <span class="ipam-chip" data-tone="muted">SRV {{ stats.typeCount.get('SRV') ?? 0 }}</span>
              <span class="ipam-chip" data-tone="muted">MX {{ stats.typeCount.get('MX') ?? 0 }}</span>
            </div>
          </div>

          <template v-if="recTab === 'rec'">
            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">关键字</span>
                <Input v-model:value="recKeyword" allow-clear placeholder="记录名 / 完整域名 / 记录值" style="width: 240px" />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">类型</span>
                <Select v-model:value="recTypeFilter" :options="typeOptions" style="width: 120px" />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">状态</span>
                <Select
                  v-model:value="recStatus"
                  :options="[
                    { label: '全部状态', value: 'all' },
                    { label: '已启用', value: 'enabled' },
                    { label: '已停用', value: 'disabled' },
                  ]"
                  style="width: 120px"
                />
              </div>
              <Button size="small" @click="resetRecFilters">重置</Button>
            </div>

            <Table
              :data-source="recFiltered"
              :loading="loading"
              :columns="[
                { title: '记录（相对名 / 完整域名）', dataIndex: 'name', minWidth: 260 },
                { title: '类型', dataIndex: 'recType', width: 80 },
                { title: 'TTL', dataIndex: 'ttl', width: 90 },
                { title: '记录值', dataIndex: 'rdata', minWidth: 200 },
                { title: '启用', dataIndex: 'enabled', width: 80 },
                { title: '操作', key: 'op', width: 140 },
              ]"
              :locale="{ emptyText: records.length === 0 ? '该区域暂无解析记录，点击「新增记录」创建' : '没有匹配的解析记录' }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              row-key="id"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'name'">
                  <div class="ipam-cell-stack">
                    <span class="ipam-cell-title ipam-mono">{{ record.name }}</span>
                    <span class="ipam-cell-meta ipam-mono">{{ fqdnOf(record.name) }}</span>
                  </div>
                </template>
                <template v-else-if="column.dataIndex === 'recType'">
                  <Tag class="m-0" :color="record.recType === 'AAAA' ? 'cyan' : record.recType === 'CNAME' ? 'purple' : record.recType === 'TXT' ? 'green' : record.recType === 'SRV' || record.recType === 'MX' ? 'orange' : 'blue'">
                    {{ record.recType }}
                  </Tag>
                </template>
                <template v-else-if="column.dataIndex === 'ttl'">
                  <span class="ipam-hint">{{ record.ttl }}s</span>
                </template>
                <template v-else-if="column.dataIndex === 'rdata'">
                  <span class="ipam-mono text-xs">{{ record.rdata }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'enabled'">
                  <Switch :checked="record.enabled" size="small" @change="(v: any) => toggleRecord(record as DnsRecord, !!v)" />
                </template>
                <template v-else-if="column.key === 'op'">
                  <div class="flex items-center gap-1">
                    <Button size="small" @click="openEditRecord(record as DnsRecord)">编辑</Button>
                    <Popconfirm title="确认删除该解析记录？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="removeRecord(record as DnsRecord)">
                      <Button size="small" danger>删除</Button>
                    </Popconfirm>
                  </div>
                </template>
              </template>
            </Table>
          </template>

          <template v-else>
            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">关键字</span>
                <Input v-model:value="linkedKeyword" allow-clear placeholder="记录名 / 值 / 来源 MAC" style="width: 240px" />
              </div>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">只读 · 由 DHCP 双栈联动维护</span>
            </div>

            <div class="ipam-note mb-3">
              <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
              <span>
                联动记录由 DHCP 双栈联动自动生成：终端通过 DHCP 获取地址时，控制面按「主机名 → IP」自动派生
                A / AAAA 记录并随租约实时同步。此处只读，修改请到 DHCP 侧变更主机名或绑定。
              </span>
            </div>

            <Table
              :data-source="linkedFiltered"
              :loading="loading"
              :columns="[
                { title: '名称', dataIndex: 'name', minWidth: 200 },
                { title: '类型', dataIndex: 'recType', width: 90 },
                { title: '值', dataIndex: 'rdata', minWidth: 200 },
                { title: '来源 MAC', dataIndex: 'mac', minWidth: 160 },
              ]"
              :locale="{ emptyText: linked.length === 0 ? '该区域暂无联动记录' : '没有匹配的联动记录' }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              row-key="name"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'name'">
                  <span class="ipam-mono text-xs">{{ record.name }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'recType'">
                  <Tag class="m-0" :color="record.recType === 'AAAA' ? 'cyan' : 'blue'">{{ record.recType }}</Tag>
                </template>
                <template v-else-if="column.dataIndex === 'rdata'">
                  <span class="ipam-mono text-xs">{{ record.rdata }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'mac'">
                  <span class="ipam-chip ipam-mono" data-tone="muted">{{ record.mac || '—' }}</span>
                </template>
              </template>
            </Table>
          </template>
        </div>
        <div v-else class="ipam-empty">
          <div class="ipam-empty__title">尚未选择区域</div>
          <div class="ipam-empty__desc">在左侧选择一个区域查看解析记录，或点击「新建区域」创建</div>
        </div>
      </Card>
    </div>

    <!-- 新增记录弹窗 -->
    <AddRecordModal class="w-[680px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">记录名称<em>*</em></span>
          <Input v-model:value="recForm.name" placeholder="如 www 或 api.corp.local" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">类型<em>*</em></span>
          <Select v-model:value="recForm.recType" :options="REC_TYPES.map((v) => ({ value: v, label: v }))" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">记录值<em>*</em></span>
          <Input v-model:value="recForm.rdata" placeholder="A/AAAA 填 IP；CNAME/PTR 填域名（尾点）" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">TTL（秒）</span>
          <InputNumber v-model:value="recForm.ttl" :min="1" :max="86400" style="width: 100%" />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">解析后完整域名</span>
          <div class="flex h-8 items-center">
            <span class="ipam-chip ipam-mono" data-tone="blue">{{ fqdnOf(recForm.name || '记录名') }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          记录创建后立即可解析；若与联动记录冲突，静态记录优先。
        </div>
      </div>
    </AddRecordModal>

    <!-- 记录编辑弹窗 -->
    <RecordModal class="w-[680px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">名称<em>*</em></span>
          <Input v-model:value="recordEdit.name" placeholder="如 www 或 api.corp.local" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">类型</span>
          <Select v-model:value="recordEdit.recType" :options="REC_TYPES.map((v) => ({ value: v, label: v }))" />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">记录值<em>*</em></span>
          <Input v-model:value="recordEdit.rdata" placeholder="A=IP / CNAME=域名（尾点）" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">TTL（秒）</span>
          <InputNumber v-model:value="recordEdit.ttl" :min="1" :max="86400" style="width: 100%" />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">启用</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="recordEdit.enabled" size="small" />
            <span class="ipam-hint">{{ recordEdit.enabled ? '参与解析应答' : '保留配置但不参与应答' }}</span>
          </div>
        </div>
      </div>
    </RecordModal>

    <!-- zone 弹窗 -->
    <ZoneModal class="w-[600px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">域名<em>*</em></span>
          <Input v-model:value="zoneForm.name" placeholder="如 office.local 或 crphbz.com" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">区域类型</span>
          <Select
            v-model:value="zoneForm.kind"
            :options="[
              { value: 'auth', label: 'auth（权威）' },
              { value: 'local', label: 'local（本地）' },
            ]"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          权威区域对外应答；本地区域仅对内网客户端生效。域名保存时自动补尾点。
        </div>
      </div>
    </ZoneModal>

    <Modal
      v-model:open="hostsOpen"
      title="导入 hosts"
      ok-text="开始导入"
      cancel-text="取消"
      :confirm-loading="importing"
      @ok="submitHostsImport"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">hosts 内容（IP name [alias...]）</span>
          <Input.TextArea
            v-model:value="hostsContent"
            :rows="10"
            class="ipam-mono"
            placeholder="# 注释&#10;10.0.0.10 app.corp.local app&#10;fd00::10 v6.corp.local"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          每个名称创建 A/AAAA 记录（TTL 300）；重复记录自动跳过；导入后自动重渲染 zonefile。
        </div>
      </div>
    </Modal>
  </div>
</template>
