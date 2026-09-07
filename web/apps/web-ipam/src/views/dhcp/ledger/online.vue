<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import { Input, message } from 'ant-design-vue';

import {
  listLedger,
  listSubnets,
  type LedgerRow,
  type Subnet,
} from '#/api/ipam';

interface OnlineRow extends LedgerRow {
  subnetCidr: string;
  familyLabel: string;
}

const rows = ref<OnlineRow[]>([]);
const subnetMap = ref<Map<string, Subnet>>(new Map());
const loading = ref(false);
const search = ref('');
const familyTab = ref<'all' | 'v4' | 'v6'>('all');

// ── 模糊搜索（IP / MAC / 主机名）+ 分族 ──
const listRows = computed(() => {
  const kw = search.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (familyTab.value !== 'all' && r.family !== (familyTab.value === 'v6' ? 6 : 4)) {
      return false;
    }
    if (!kw) return true;
    return [r.address, r.mac, r.hostname].some((v) =>
      (v ?? '').toLowerCase().includes(kw),
    );
  });
});

async function load() {
  loading.value = true;
  try {
    // 先取子网做 id→CIDR 关联（地址规划网段对应）
    const subs = (await listSubnets()).items ?? [];
    subnetMap.value = new Map(subs.map((s) => [s.id, s]));
    const [v4, v6] = await Promise.all([
      listLedger({ family: 4, state: 'online', pageSize: 500 }),
      listLedger({ family: 6, state: 'online', pageSize: 500 }),
    ]);
    const map = (items: LedgerRow[] | undefined, family: 4 | 6) =>
      (items ?? []).map((r) => ({
        ...r,
        subnetCidr: subnetMap.value.get(r.subnetId ?? '')?.cidr ?? '—',
        familyLabel: `IPv${family}`,
      }));
    rows.value = [...map(v4.items, 4), ...map(v6.items, 6)];
  } catch (e) {
    message.error(e instanceof Error ? e.message : '加载失败');
  } finally {
    loading.value = false;
  }
}

onMounted(load);

const listGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'address', title: '在线地址', minWidth: 150 },
    { field: 'familyLabel', title: '族', width: 80 },
    { field: 'subnetCidr', title: '所属子网', minWidth: 150 },
    { field: 'mac', title: 'MAC', minWidth: 150, slots: { default: 'mac' } },
    { field: 'hostname', title: '主机名', minWidth: 120, slots: { default: 'hostname' } },
    { field: 'leaseStart', title: '租用时间', minWidth: 150, slots: { default: 'leaseStart' } },
    { field: 'leaseExpiry', title: '到期时间', minWidth: 150, slots: { default: 'leaseExpiry' } },
  ],
  loading: loading.value,
  rowConfig: { keyField: 'address' },
});
const [OnlineGrid] = useVbenVxeGrid({ gridOptions: listGridOptions });
</script>

<template>
  <div class="p-4">
    <Card title="在线地址列表（DHCP 租约活跃）">
      <template #extra>
        <Input
          v-model:value="search"
          allow-clear
          placeholder="按 IP / MAC / 主机名 模糊搜索"
          style="width: 320px"
        />
      </template>
      <div class="mb-3 flex items-center gap-2">
        <RadioGroup
          v-model:value="familyTab"
          option-type="button"
          size="small"
          :options="[
            { label: `全部（${rows.length}）`, value: 'all' },
            { label: `IPv4（${rows.filter((r) => r.family === 4).length}）`, value: 'v4' },
            { label: `IPv6（${rows.filter((r) => r.family === 6).length}）`, value: 'v6' },
          ]"
        />
      </div>
      <OnlineGrid :table-data="listRows">
        <template #mac="{ row }">
          <span v-if="row.mac" class="font-mono">{{ row.mac }}</span>
          <span v-else>-</span>
        </template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #leaseStart="{ row }">
          {{ row.leaseStart ? new Date(row.leaseStart).toLocaleString() : '-' }}
        </template>
        <template #leaseExpiry="{ row }">
          {{ row.leaseExpiry ? new Date(row.leaseExpiry).toLocaleString() : '-' }}
        </template>
      </OnlineGrid>
    </Card>
  </div>
</template>
