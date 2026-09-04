<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenModal } from '@vben/common-ui';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import {
  Button,
  Card,
  Input,
  Modal,
  RadioGroup,
  Select,
  Tag,
  message,
} from 'ant-design-vue';

import {
  bindStatic,
  bulkReservations,
  listLedger,
  type LedgerRow,
  listSubnets,
  releaseAddress,
  type ReservationBulkEntryIn,
  type ReservationBulkResult,
  type Subnet,
  updateBinding,
} from '#/api/ipam';
import { normalizeMacInput } from '#/utils/mac';

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

// ── 模糊搜索（IP / MAC / 主机名，大小写不敏感） ──
const search = ref('');
function filterRows(rows: LedgerRow[]): LedgerRow[] {
  const kw = search.value.trim().toLowerCase();
  if (!kw) return rows;
  return rows.filter((r) =>
    [r.address, r.mac, r.hostname].some((v) => (v ?? '').toLowerCase().includes(kw)),
  );
}
const resV4 = computed(() => filterRows(resRows.value).filter((r) => r.family === 4));
const resV6 = computed(() => filterRows(resRows.value).filter((r) => r.family === 6));
const bindV4 = computed(() => filterRows(bindRows.value).filter((r) => r.family === 4));
const bindV6 = computed(() => filterRows(bindRows.value).filter((r) => r.family === 6));
const resTab = ref('v4');
const bindTab = ref('v4');

async function loadLists() {
  listLoading.value = true;
  try {
    const [r, b] = await Promise.all([
      listLedger({ state: 'reserved' }),
      listLedger({ state: 'static' }),
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
  const macRej = entries.value.findIndex(
    (r) => r.kind === 'bind' && !normalizeMacInput(r.mac),
  );
  if (macRej >= 0) {
    message.warning(
      `第 ${macRej + 1} 行 MAC 格式不合法（支持 C4-3D-1A-07-EB-2B / C43D1A07EB2B / 冒号分隔，大小写均可）`,
    );
    return;
  }
  submitting.value = true;
  try {
    const payload: ReservationBulkEntryIn[] = entries.value.map((r) => ({
      address: r.address,
      kind: r.kind,
      ...(r.kind === 'bind' ? { mac: normalizeMacInput(r.mac) ?? r.mac } : {}),
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

const resGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'address', title: '地址', minWidth: 160 },
    { field: 'family', title: '族', width: 70 },
    { field: 'mac', title: 'MAC', minWidth: 160, slots: { default: 'mac' } },
    { field: 'hostname', title: '主机名', minWidth: 120, slots: { default: 'hostname' } },
    { field: 'actions', title: '操作', width: 90, fixed: 'right', slots: { default: 'resActions' } },
  ],
  loading: listLoading.value,
  rowConfig: { keyField: 'address' },
});
const [ResGrid] = useVbenVxeGrid({ gridOptions: resGridOptions });

const bindGridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'address', title: '地址', minWidth: 160 },
    { field: 'family', title: '族', width: 70 },
    { field: 'mac', title: 'MAC', minWidth: 160, slots: { default: 'mac' } },
    { field: 'hostname', title: '主机名', minWidth: 120, slots: { default: 'hostname' } },
    { field: 'actions', title: '操作', width: 140, fixed: 'right', slots: { default: 'bindActions' } },
  ],
  loading: listLoading.value,
  rowConfig: { keyField: 'address' },
});
const [BindGrid] = useVbenVxeGrid({ gridOptions: bindGridOptions });

// ── 释放 ──
async function releaseRow(row: LedgerRow) {
  Modal.confirm({
    title: `释放 ${row.address}`,
    content: row.mac
      ? `静态绑定（MAC ${row.mac}）将被删除，该地址回归动态池可正常下发。在线租约不受影响。`
      : '保留（冻结）将被取消，该地址回归动态池可正常下发。',
    okText: '释放',
    okType: 'danger',
    onOk: async () => {
      try {
        await releaseAddress(row.address);
        message.success(`${row.address} 已释放`);
      } catch (e) {
        message.error(e instanceof Error ? e.message : '释放失败');
      } finally {
        await loadLists();
      }
    },
  });
}

// ── 编辑静态绑定 ──
const editTarget = ref<LedgerRow>();
const editForm = reactive({ address: '', mac: '' });
const [EditModal, editModalApi] = useVbenModal({
  draggable: true,
  confirmText: '保存',
  onConfirm: () => confirmEdit(),
});

function openEdit(row: LedgerRow) {
  editTarget.value = row;
  editForm.address = row.address;
  editForm.mac = row.mac || '';
  editModalApi.setState({
    title: row.subnetId ? `编辑绑定 · ${row.address}` : `编辑 MAC · ${row.address}（无子网信息仅可改 MAC）`,
  });
  editModalApi.open();
}

async function confirmEdit() {
  const row = editTarget.value;
  if (!row) return;
  const normMac = normalizeMacInput(editForm.mac);
  const addr = editForm.address.trim();
  if (!normMac) {
    message.warning(
      'MAC 格式不合法（支持 C4-3D-1A-07-EB-2B / C43D1A07EB2B / 冒号分隔，大小写均可）',
    );
    return;
  }
  try {
    if (addr === row.address) {
      await updateBinding(row.address, normMac);
    } else {
      if (!row.subnetId) {
        message.warning('该记录缺少子网信息，仅可修改 MAC');
        return;
      }
      await releaseAddress(row.address);
      await bindStatic(row.subnetId, addr, normMac);
    }
    message.success(`${addr} 绑定已更新（${normMac}）`);
    editModalApi.close();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '更新失败');
  } finally {
    await loadLists();
  }
}

</script>

<template>
  <div class="grid grid-cols-1 gap-4 p-4">
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

    <div class="mb-2 flex items-center gap-2">
      <span class="text-xs text-gray-400">搜索</span>
      <Input
        v-model:value="search"
        allow-clear
        placeholder="按 IP / MAC / 主机名 模糊搜索（大小写不敏感）"
        style="max-width: 380px"
      />
      <span v-if="search" class="text-xs text-gray-400">
        命中：保留 v4 {{ resV4.length }} / v6 {{ resV6.length }} · 绑定 v4 {{ bindV4.length }} / v6 {{ bindV6.length }}
      </span>
    </div>

    <Card title="保留列表（冻结不下发）">
      <RadioGroup
        v-model:value="resTab"
        option-type="button"
        size="small"
        class="mb-2"
        :options="[{ label: 'IPv4', value: 'v4' }, { label: 'IPv6', value: 'v6' }]"
      />
      <ResGrid v-if="resTab === 'v4'" :table-data="resV4">
        <template #mac="{ row }">{{ row.mac || '-' }}</template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #resActions="{ row }">
          <Button size="small" danger @click="releaseRow(row)">释放</Button>
        </template>
      </ResGrid>
      <ResGrid v-else :table-data="resV6">
        <template #mac="{ row }">{{ row.mac || '-' }}</template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #resActions="{ row }">
          <Button size="small" danger @click="releaseRow(row)">释放</Button>
        </template>
      </ResGrid>
    </Card>

    <Card title="静态绑定列表（MAC↔IP）">
      <RadioGroup
        v-model:value="bindTab"
        option-type="button"
        size="small"
        class="mb-2"
        :options="[{ label: 'IPv4', value: 'v4' }, { label: 'IPv6', value: 'v6' }]"
      />
      <BindGrid v-if="bindTab === 'v4'" :table-data="bindV4">
        <template #mac="{ row }">
          <Tag class="font-mono">{{ row.mac || '-' }}</Tag>
        </template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #bindActions="{ row }">
          <Button size="small" class="mr-2" @click="openEdit(row)">编辑</Button>
          <Button size="small" danger @click="releaseRow(row)">释放</Button>
        </template>
      </BindGrid>
      <BindGrid v-else :table-data="bindV6">
        <template #mac="{ row }">
          <Tag class="font-mono">{{ row.mac || '-' }}</Tag>
        </template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #bindActions="{ row }">
          <Button size="small" class="mr-2" @click="openEdit(row)">编辑</Button>
          <Button size="small" danger @click="releaseRow(row)">释放</Button>
        </template>
      </BindGrid>
    </Card>

    <EditModal>
      <div class="flex flex-col gap-3">
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-gray-400">地址</span>
          <Input
            v-model:value="editForm.address"
            :disabled="!editTarget?.subnetId"
            style="width: 220px"
            placeholder="如 10.61.172.12"
          />
          <span v-if="!editTarget?.subnetId" class="text-xs text-gray-400">无子网信息，仅可改 MAC</span>
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-gray-400">MAC</span>
          <Input v-model:value="editForm.mac" style="width: 220px" placeholder="如 aa:bb:cc:dd:ee:02" />
        </div>
        <div class="text-xs text-gray-400">修改地址 = 释放旧地址并在新地址重新绑定；释放后原地址回归动态池。</div>
      </div>
    </EditModal>
  </div>
</template>
