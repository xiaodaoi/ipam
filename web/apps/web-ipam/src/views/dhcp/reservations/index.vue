<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import {
  Button,
  Card,
  Input,
  Select,
  Table,
  Tag,
  message,
} from 'ant-design-vue';

import {
  bulkReservations,
  listLedger,
  type LedgerRow,
  listSubnets,
  type ReservationBulkEntryIn,
  type ReservationBulkResult,
  type Subnet,
} from '#/api/ipam';

const subnets = ref<Subnet[]>([]);
const selectedSubnet = ref<string>();
type RowDraft = { address: string; kind: 'bind' | 'reserve'; mac: string; reason: string };
const entries = ref<RowDraft[]>([{ address: '', kind: 'reserve', mac: '', reason: '' }]);
const bulkResult = ref<ReservationBulkResult>();
const submitting = ref(false);
const resRows = ref<LedgerRow[]>([]);
const bindRows = ref<LedgerRow[]>([]);
const listLoading = ref(false);

const subnetOptions = computed(() =>
  subnets.value.map((s) => ({ label: `${s.name}（${s.cidr}）`, value: s.id })),
);

async function loadLists() {
  listLoading.value = true;
  try {
    const [r, b] = await Promise.all([
      listLedger({ state: 'reserved' }),
      listLedger({ state: 'bound' }),
    ]);
    resRows.value = r.items ?? [];
    bindRows.value = b.items ?? [];
  } finally {
    listLoading.value = false;
  }
}
async function loadSubnets() {
  const d = await listSubnets();
  subnets.value = d.items ?? [];
}

function addRow(kind: 'bind' | 'reserve') {
  entries.value.push({ address: '', kind, mac: '', reason: '' });
}
function removeRow(i: number) {
  entries.value.splice(i, 1);
}
function switchKind(i: number, kind: 'bind' | 'reserve') {
  const cur = entries.value[i];
  if (!cur) return;
  entries.value[i] = { address: cur.address, kind, mac: '', reason: '' };
}

async function submitBulk() {
  if (!selectedSubnet.value) {
    message.warning('请选择子网');
    return;
  }
  if (entries.value.length === 0 || entries.value.some((r) => !r.address)) {
    message.warning('至少一条条目且地址必填');
    return;
  }
  if (entries.value.some((r) => r.kind === 'bind' && !r.mac)) {
    message.warning('bind 条目必须填 MAC');
    return;
  }
  submitting.value = true;
  try {
    const payload: ReservationBulkEntryIn[] = entries.value.map((r) => ({
      address: r.address,
      kind: r.kind,
      ...(r.kind === 'bind' ? { mac: r.mac } : {}),
      ...(r.reason ? { reason: r.reason } : {}),
    }));
    bulkResult.value = await bulkReservations({
      entries: payload,
      subnetId: selectedSubnet.value,
    });
    if (bulkResult.value.ok) {
      message.success(`已应用 ${bulkResult.value.applied} 条`);
      entries.value = [{ address: '', kind: 'reserve', mac: '', reason: '' }];
    } else {
      message.error('存在失败行，整体已回滚');
    }
  } catch (e) {
    message.error(e instanceof Error ? e.message : '提交失败');
  } finally {
    submitting.value = false;
  }
  await loadLists();
}
onMounted(() => {
  void loadLists();
  void loadSubnets();
});
</script>

<template>
  <div class="grid grid-cols-1 gap-4">
    <Card title="批量创建（CSV 语义，事务性：任一失败整体回滚）">
      <div class="mb-3 flex flex-wrap items-center gap-2">
        <span class="text-xs text-gray-400">目标子网</span>
        <Select
          v-model:value="selectedSubnet"
          :options="subnetOptions"
          placeholder="选择子网"
          show-search
          style="width: 320px"
        />
        <Button @click="addRow('reserve')">+ 保留行</Button>
        <Button @click="addRow('bind')">+ 绑定行</Button>
        <Button type="primary" :loading="submitting" @click="submitBulk">提交批量</Button>
      </div>
      <div v-for="(r, i) in entries" :key="i" class="mb-2 flex flex-wrap items-center gap-2">
        <Select
          :options="[{ value: 'reserve', label: '保留（冻结）' }, { value: 'bind', label: '静态绑定' }]"
          :value="r.kind"
          style="width: 130px"
          @change="(v: any) => switchKind(i, v)"
        />
        <Input v-model:value="r.address" style="width: 200px" placeholder="地址 如 10.61.172.12" />
        <Input v-if="r.kind === 'bind'" v-model:value="r.mac" style="width: 180px" placeholder="MAC 如 aa:bb:cc:dd:ee:02" />
        <Input v-model:value="r.reason" style="width: 180px" placeholder="原因（可选）" />
        <Button size="small" danger @click="removeRow(i)">移除</Button>
      </div>
      <div v-if="bulkResult && !bulkResult.ok" class="mt-2 rounded bg-red-50 p-2 text-xs text-red-600">
        回滚明细（无任何写入）：
        <div v-for="f in bulkResult.failures" :key="f.line">
          第 {{ f.line }} 行：{{ f.reason }}
        </div>
      </div>
      <div v-else-if="bulkResult?.ok" class="mt-2 text-xs text-gray-500">
        已应用 {{ bulkResult.applied }} 条。
      </div>
    </Card>

    <Card title="保留列表（冻结不下发）">
      <Table
        :data-source="resRows"
        :columns="[
          { title: '地址', dataIndex: 'address' },
          { title: '族', dataIndex: 'family', width: 60 },
          { title: 'MAC', dataIndex: 'mac' },
          { title: '主机名', dataIndex: 'hostname' },
        ]"
        row-key="address"
        size="small"
        :loading="listLoading"
        :pagination="{ pageSize: 10 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'mac'">{{ record.mac || '-' }}</template>
          <template v-else-if="column.dataIndex === 'hostname'">{{ record.hostname || '-' }}</template>
        </template>
      </Table>
    </Card>

    <Card title="静态绑定列表（MAC↔IP）">
      <Table
        :data-source="bindRows"
        :columns="[
          { title: '地址', dataIndex: 'address' },
          { title: '族', dataIndex: 'family', width: 60 },
          { title: 'MAC', dataIndex: 'mac' },
          { title: '主机名', dataIndex: 'hostname' },
        ]"
        row-key="address"
        size="small"
        :loading="listLoading"
        :pagination="{ pageSize: 10 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'mac'">
            <Tag class="font-mono">{{ record.mac || '-' }}</Tag>
          </template>
          <template v-else-if="column.dataIndex === 'hostname'">{{ record.hostname || '-' }}</template>
        </template>
      </Table>
    </Card>
  </div>
</template>