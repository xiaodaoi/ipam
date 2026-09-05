<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import {
  Button,
  Card,
  Empty,
  Input,
  InputNumber,
  Modal,
  Select,
  RadioGroup,
  Switch,
  Tag,
  message,
} from 'ant-design-vue';

import {
  createDnsRecord,
  createDnsZone,
  deleteDnsRecord,
  deleteDnsZone,
  listDnsRecords,
  listDnsZones,
  listLinkedRecords,
  updateDnsRecord,
  updateDnsZone,
  type DnsRecord,
  type DnsZone,
} from '#/api/ipam';

type RecType = 'A' | 'AAAA' | 'CNAME' | 'PTR';

const zones = ref<DnsZone[]>([]);
const zoneId = ref<string>();
const records = ref<DnsRecord[]>([]);
const linked = ref<any[]>([]);
const loading = ref(false);

const activeZone = computed(() => zones.value.find((z) => z.id === zoneId.value));

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
  void loadRecords();
}

const recTab = ref<'rec' | 'linked'>('rec');

// ── 新建记录表单 ──
const form = reactive<{ name: string; recType: RecType; rdata: string; ttl: number }>({
  name: '', recType: 'A', rdata: '', ttl: 300,
});

async function addRecord() {
  if (!zoneId.value) {
    message.warning('请先选择区域');
    return;
  }
  if (!form.name || !form.rdata) {
    message.warning('请填写记录名称与记录值');
    return;
  }
  try {
    await createDnsRecord(zoneId.value, { ...form, enabled: true });
    form.name = '';
    form.rdata = '';
    await loadRecords();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '添加失败');
  }
}

// ── 编辑记录 ──
const editRecord = ref<DnsRecord>();
const recordEdit = reactive<{ name: string; recType: RecType; rdata: string; ttl: number; enabled: boolean }>({
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
  } catch (e) {
    message.error(e instanceof Error ? e.message : '更新失败');
  }
}

async function toggleRecord(r: DnsRecord, enabled: boolean) {
  if (!zoneId.value) return;
  try {
    await updateDnsRecord(zoneId.value, r.id, { enabled });
    await loadRecords();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
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
      } catch (e) {
        message.error(e instanceof Error ? e.message : '删除失败');
      }
    },
  });
}

// ── 新建/编辑 zone ──
const zoneForm = reactive<{ name: string; kind: 'auth' | 'local' }>({ name: '', kind: 'auth' });
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
  zoneModalApi.setState({ title: '新建 DNS 区域' });
  zoneModalApi.open();
}

function openEditZone(z: DnsZone) {
  editingZoneId.value = z.id;
  zoneForm.name = z.name.replace(/\.$/, '');
  zoneForm.kind = z.kind;
  zoneModalApi.setState({ title: `编辑区域 · ${z.name}` });
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
  } catch (e) {
    message.error(e instanceof Error ? e.message : '保存失败');
  }
}

async function toggleZone(z: DnsZone, enabled: boolean) {
  try {
    await updateDnsZone(z.id, { enabled });
    await loadZones();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
}

function removeZone(z: DnsZone) {
  Modal.confirm({
    title: `删除区域 ${z.name}`,
    content: '其下全部解析记录将一并删除，且从 unbound 配置移除。',
    okText: '删除',
    okType: 'danger',
    onOk: async () => {
      try {
        await deleteDnsZone(z.id);
        if (zoneId.value === z.id) zoneId.value = undefined;
        message.success('区域已删除');
        await loadZones();
      } catch (e) {
        message.error(e instanceof Error ? e.message : '删除失败');
      }
    },
  });
}

onMounted(loadZones);

// ── 记录表格 ──
const recGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '记录名（相对名或 FQDN）', minWidth: 160, showOverflow: true },
    { field: 'recType', title: '类型', width: 80 },
    { field: 'ttl', title: 'TTL', width: 70 },
    { field: 'rdata', title: '记录值', minWidth: 140, showOverflow: true },
    { field: 'enabled', title: '启用', width: 70, slots: { default: 'enabled' } },
    { field: 'op', title: '操作', width: 130, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  rowConfig: { keyField: 'id' },
});
const [RecGrid] = useVbenVxeGrid({ gridOptions: recGridOptions });

const linkedGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', minWidth: 160 },
    { field: 'recType', title: '类型', width: 80 },
    { field: 'rdata', title: '值', minWidth: 140 },
    { field: 'mac', title: '来源 MAC', minWidth: 140 },
  ],
  rowConfig: { keyField: 'name' },
});
const [LinkGrid] = useVbenVxeGrid({ gridOptions: linkedGridOptions });
</script>

<template>
  <div class="flex gap-4 p-4">
    <!-- 左侧 zone 列表 -->
    <Card class="w-64 shrink-0 self-start" title="DNS 区域" :body-style="{ padding: '8px' }">
      <template #extra>
        <Button size="small" type="primary" @click="openCreateZone">+ 新建</Button>
      </template>
      <div v-if="!zones.length" class="py-8 text-center text-muted-foreground">
        <Empty description="暂无区域" :image-style="{ height: '40px' }" />
      </div>
      <div
        v-for="z in zones"
        :key="z.id"
        class="mb-1 cursor-pointer rounded px-2 py-2 transition-colors"
        :class="zoneId === z.id ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/60'"
        @click="selectZone(z.id)"
      >
        <div class="flex items-center justify-between gap-1">
          <span class="truncate text-sm font-medium" :title="z.name">{{ z.name }}</span>
          <Tag :color="z.kind === 'auth' ? 'blue' : 'purple'" class="mr-0">{{ z.kind }}</Tag>
        </div>
        <div class="mt-1 flex items-center justify-between">
          <span class="text-xs text-muted-foreground">{{ z.enabled ? '启用中' : '已停用' }}</span>
          <div class="flex items-center gap-1" @click.stop>
            <Switch :checked="z.enabled" size="small" @change="(v) => toggleZone(z as DnsZone, Boolean(v))" />
            <Button size="small" type="text" @click="openEditZone(z)">改</Button>
            <Button size="small" type="text" danger @click="removeZone(z)">删</Button>
          </div>
        </div>
      </div>
    </Card>

    <!-- 右侧记录 -->
    <Card class="min-w-0 flex-1">
      <template #title>
        <span>解析记录 {{ activeZone ? `· ${activeZone.name}` : '' }}</span>
      </template>
      <div v-if="activeZone">
        <div class="mb-3">
          <RadioGroup
            v-model:value="recTab"
            option-type="button"
            size="small"
            :options="[
              { label: `静态记录（${records.length}）`, value: 'rec' },
              { label: `联动记录（${linked.length}）`, value: 'linked' },
            ]"
          />
        </div>

        <template v-if="recTab === 'rec'">
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <span class="text-xs text-muted-foreground">新增</span>
            <Input v-model:value="form.name" placeholder="名称 如 www 或 api.corp.local" style="width: 220px" />
            <Select
              v-model:value="form.recType"
              style="width: 90px"
              :options="(['A', 'AAAA', 'CNAME', 'PTR'] as RecType[]).map((v) => ({ value: v, label: v }))"
            />
            <Input v-model:value="form.rdata" placeholder="记录值（A/IP、CNAME/域名尾点）" style="width: 240px" />
            <span class="text-xs text-muted-foreground">TTL</span>
            <InputNumber v-model:value="form.ttl" :min="1" :max="86400" style="width: 90px" />
            <Button type="primary" size="small" @click="addRecord">添加</Button>
          </div>
          <RecGrid :table-data="records">
            <template #enabled="{ row }">
              <Switch :checked="row.enabled" size="small" @change="(v) => toggleRecord(row as DnsRecord, Boolean(v))" />
            </template>
            <template #op="{ row }">
              <div class="flex items-center gap-1">
                <Button size="small" @click="openEditRecord(row as DnsRecord)">编辑</Button>
                <Button size="small" danger @click="removeRecord(row as DnsRecord)">删除</Button>
              </div>
            </template>
          </RecGrid>
        </template>

        <template v-else>
          <div class="mb-2 rounded bg-muted p-2 text-xs text-muted-foreground">
            联动记录由 DHCP 双栈联动自动生成（§4.4）：当终端通过 DHCP 获取地址时，控制面按「主机名 → IP」自动派生
            A/AAAA 记录并随租约/绑定实时同步，用于内网按主机名访问。只读不可在此编辑——修改请到 DHCP 侧变更主机名或绑定。
          </div>
          <LinkGrid :table-data="linked" />
        </template>
      </div>
      <div v-else class="py-12 text-center text-muted-foreground">请先在左侧选择或新建区域</div>
    </Card>

    <!-- 记录编辑弹窗 -->
    <RecordModal>
      <div class="flex flex-col gap-3">
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">名称</span>
          <Input v-model:value="recordEdit.name" style="width: 240px" placeholder="如 www 或 api.corp.local" />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">类型</span>
          <Select
            v-model:value="recordEdit.recType"
            style="width: 120px"
            :options="(['A', 'AAAA', 'CNAME', 'PTR'] as RecType[]).map((v) => ({ value: v, label: v }))"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">记录值</span>
          <Input v-model:value="recordEdit.rdata" style="width: 240px" placeholder="A=IP / CNAME=域名（尾点）" />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">TTL</span>
          <InputNumber v-model:value="recordEdit.ttl" :min="1" :max="86400" style="width: 120px" />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">启用</span>
          <Switch v-model:checked="recordEdit.enabled" />
        </div>
      </div>
    </RecordModal>

    <!-- zone 弹窗 -->
    <ZoneModal>
      <div class="flex flex-col gap-3">
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">域名</span>
          <Input v-model:value="zoneForm.name" style="width: 260px" placeholder="如 office.local 或 crphbz.com" />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-muted-foreground">类型</span>
          <Select
            v-model:value="zoneForm.kind"
            style="width: 140px"
            :options="[
              { value: 'auth', label: 'auth（权威）' },
              { value: 'local', label: 'local（本地）' },
            ]"
          />
        </div>
      </div>
    </ZoneModal>
  </div>
</template>
