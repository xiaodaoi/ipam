<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import { Button, Card, Input, Select,  TabPane, Tabs, message } from 'ant-design-vue';

import {
  createDnsRecord,
  createDnsZone,
  deleteDnsRecord,
  deleteDnsZone,
  listDnsRecords,
  listDnsZones,
  listLinkedRecords,
  type DnsRecord,
  type DnsZone,
} from '#/api/ipam';

const zones = ref<DnsZone[]>([]);
const zoneId = ref<string>();
const records = ref<DnsRecord[]>([]);
const linked = ref<any[]>([]);
const loading = ref(false);
type RecType = 'A' | 'AAAA' | 'CNAME' | 'PTR';
const form = ref<{ name: string; recType: RecType; rdata: string; ttl: number }>({
  name: '', recType: 'A', rdata: '', ttl: 300,
});

const activeZone = computed(() => zones.value.find((z) => z.id === zoneId.value));

async function loadZones() {
  const d = await listDnsZones();
  zones.value = d.items ?? [];
  if (!zoneId.value && zones.value.length) zoneId.value = zones.value[0]!.id;
  if (zoneId.value) await loadRecords();
}
async function loadRecords() {
  if (!zoneId.value) return;
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
const zoneModal = ref({ name: '' });
const [ZoneModal, zoneModalApi] = useVbenModal({ draggable: true, title: '新建 DNS 区域', confirmText: '创建', onConfirm: () => addRecord() });
async function createZoneConfirm() {
  const name = zoneModal.value.name.trim();
  if (!name) return;
  await createDnsZone({ name: name.endsWith('.') ? name : `${name}.`, kind: 'auth' });
  zoneModalApi.close();
  zoneModal.value.name = '';
  await loadZones();
}
async function addRecord() {
  if (!zoneId.value) {
    message.warning('请先选择 DNS 区域（无可选区域时先在上方新建 zone）');
    return;
  }
  if (!form.value.name || !form.value.rdata) {
    message.warning('请填写记录名称与记录值');
    return;
  }
  await createDnsRecord(zoneId.value, { ...form.value, enabled: true });
  form.value = { name: '', recType: 'A', rdata: '', ttl: 300 };
  await loadRecords();
}
async function removeRecord(id?: string) {
  if (id && zoneId.value) await deleteDnsRecord(zoneId.value, id);
  await loadRecords();
}
async function removeZone() {
  const z = activeZone.value;
  if (!z) return;
  if (!window.confirm(`删除区域 ${z.name}？其下记录将一并删除。`)) return;
  await deleteDnsZone(z.id);
  zoneId.value = undefined;
  await loadZones();
  message.success('区域已删除');
}
onMounted(loadZones);

const recGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', width: 200 },
    { field: 'recType', title: '类型', width: 80 },
    { field: 'ttl', title: 'TTL', width: 80 },
    { field: 'rdata', title: '值', width: 360 },
    { field: 'op', title: '操作', width: 100, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  height: 'auto',
  rowConfig: { keyField: 'id' },
});
const [RecGrid] = useVbenVxeGrid({ gridOptions: recGridOptions });

const linkedGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', width: 200 },
    { field: 'recType', title: '类型', width: 80 },
    { field: 'rdata', title: '值', width: 360 },
    { field: 'mac', title: '来源 MAC', width: 160 },
  ],
  height: 'auto',
  rowConfig: { keyField: 'name' },
});
const [LinkGrid] = useVbenVxeGrid({ gridOptions: linkedGridOptions });


</script>

<template>
  <div class="p-4">
  <Card>
    <template #title>
      <div class="flex items-center gap-3">
        <span>解析记录</span>
        <Select v-model:value="zoneId" style="width: 220px" :options="zones.map((z) => ({ value: z.id, label: z.name }))"
          @change="loadRecords()" />
        <Button size="small" danger :disabled="!zoneId" @click="removeZone">删区</Button>
        <Button size="small" @click="zoneModalApi.open()">+ 新建 zone</Button>
      </div>
    </template>
    <Tabs>
      <TabPane key="rec" :tab="`静态记录 (${records.length})`">
        <div class="mb-3 flex flex-wrap items-center gap-2">
          <Input v-model:value="form.name" placeholder="名称" style="width: 180px" />
          <Select v-model:value="form.recType" style="width: 90px"
            :options="(['A', 'AAAA', 'CNAME'] as RecType[]).map((v) => ({ value: v, label: v }))" />
          <Input v-model:value="form.rdata" placeholder="值" style="width: 200px" />
          <Input-number v-model:value="form.ttl" :min="30" style="width: 90px" />
        </div>
        <RecGrid :table-data="records">
          <template #op="{ row }">
            <div class="flex items-center gap-1">
              <Button size="small" danger @click="removeRecord(row.id)">删除</Button>
            </div>
          </template>
        </RecGrid>
      </TabPane>
      <TabPane key="linked" :tab="`联动记录（只读）(${linked.length})`">
        <div class="mb-2 text-xs text-gray-400">
          来自 DHCP 双栈联动自动生成（§4.4），随租约/绑定变化自动更新，不可编辑。
          当前 zone：{{ activeZone?.name ?? '—' }}
        </div>
        <LinkGrid :table-data="linked" />
      </TabPane>
    </Tabs>
  </Card>
  <ZoneModal>
    <Input v-model:value="zoneModal.name" placeholder="如 office.local" @pressEnter="createZoneConfirm" />
  </ZoneModal>
  </div>
</template>