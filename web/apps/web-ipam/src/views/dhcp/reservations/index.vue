<script setup lang="ts">
import type { VxeGridProps } from '@vben/plugins/vxe-table';

import type { LedgerRow, ReservationBulkEntryIn, ReservationBulkResult, Subnet } from '#/api/ipam';

import { computed, onMounted, reactive, ref, watch } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  message,
  Modal,
  RadioGroup,
  Select,
  Tag,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { bindStatic, bulkReservations, listLedger, listSubnets, releaseAddress, updateBinding } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import { $t } from '#/locales';
import { normalizeMacInput } from '#/utils/mac';

const subnets = ref<Subnet[]>([]);
const selectedSubnet = ref<string>();
type RowDraft = { address: string; kind: 'bind' | 'reserve'; mac: string; reason: string };
const entries = ref<RowDraft[]>([{ address: '', kind: 'reserve', mac: '', reason: '' }]);
const bulkResult = ref<ReservationBulkResult>();
const submitting = ref(false);
const allRows = ref<LedgerRow[]>([]);
const listLoading = ref(false);

const subnetOptions = computed(() =>
  subnets.value.map((s) => ({
    label: $t('dhcp.reservations.subnetOptionLabel', {
      name: s.name,
      cidr: s.cidr,
    }),
    value: s.id,
  })),
);

const kindOptions = computed(() => [
  { value: 'reserve', label: $t('dhcp.reservations.kindReserve') },
  { value: 'bind', label: $t('dhcp.reservations.kindBind') },
]);

// ── 模糊搜索（IP / MAC / 主机名，大小写不敏感）+ IPv4/IPv6 分族 ──
const search = ref('');
const familyTab = ref<'v4' | 'v6'>('v4');
function filterRows(rows: LedgerRow[]): LedgerRow[] {
  const kw = search.value.trim().toLowerCase();
  if (!kw) return rows;
  return rows.filter((r) =>
    [r.address, r.mac, r.hostname].some((v) => (v ?? '').toLowerCase().includes(kw)),
  );
}
const listRows = computed(() =>
  filterRows(allRows.value).filter((r) =>
    familyTab.value === 'v6' ? r.family === 6 : r.family === 4,
  ),
);

async function loadLists() {
  listLoading.value = true;
  try {
    const [r, b] = await Promise.all([
      listLedger({ state: 'reserved' }),
      listLedger({ state: 'static' }),
    ]);
    allRows.value = [...(r.items ?? []), ...(b.items ?? [])];
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
    message.warning($t('dhcp.reservations.messages.needSubnet'));
    return;
  }
  if (entries.value.length === 0 || entries.value.some((r) => !r.address)) {
    message.warning($t('dhcp.reservations.messages.needAddress'));
    return;
  }
  const macRej = entries.value.findIndex(
    (r) => r.kind === 'bind' && !normalizeMacInput(r.mac),
  );
  if (macRej !== -1) {
    message.warning(
      $t('dhcp.reservations.messages.macInvalidLine', { line: macRej + 1 }),
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
      message.success(
        $t('dhcp.reservations.messages.applied', {
          n: bulkResult.value.applied,
        }),
      );
      entries.value = [{ address: '', kind: 'reserve', mac: '', reason: '' }];
    } else {
      message.error($t('dhcp.reservations.messages.rolledBack'));
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.reservations.messages.submitFailed'),
    );
  } finally {
    submitting.value = false;
  }
  await loadLists();
}
onMounted(() => {
  void loadLists();
  void loadSubnets();
});

// ── 合并列表：保留 + 静态绑定（状态列区分）──
const listGridOptions = computed<VxeGridProps>(() => ({
  columns: [
    {
      field: 'state',
      title: $t('dhcp.reservations.col.state'),
      width: 80,
      slots: { default: 'state' },
    },
    { field: 'address', title: $t('dhcp.reservations.col.address'), minWidth: 160 },
    {
      field: 'mac',
      title: $t('dhcp.reservations.col.mac'),
      minWidth: 160,
      slots: { default: 'mac' },
    },
    {
      field: 'hostname',
      title: $t('dhcp.reservations.col.hostname'),
      minWidth: 120,
      slots: { default: 'hostname' },
    },
    {
      field: 'actions',
      title: $t('dhcp.reservations.col.actions'),
      width: 140,
      fixed: 'right',
      slots: { default: 'actions' },
    },
  ],
  loading: listLoading.value,
  rowConfig: { keyField: 'address' },
}));
const [ListGrid, listGridApi] = useVbenVxeGrid({
  gridOptions: listGridOptions.value,
});
// 语言切换后同步表头文案
watch(listGridOptions, (opts) => listGridApi.setState(opts as any));

// ── 释放 ──
async function releaseRow(row: LedgerRow) {
  Modal.confirm({
    title: $t('dhcp.reservations.confirmReleaseTitle', { address: row.address }),
    content: row.mac
      ? $t('dhcp.reservations.confirmReleaseStatic', { mac: row.mac })
      : $t('dhcp.reservations.confirmReleaseReserved'),
    okText: $t('dhcp.reservations.release'),
    okType: 'danger',
    onOk: async () => {
      try {
        await releaseAddress(row.address);
        message.success(
          $t('dhcp.reservations.messages.released', { address: row.address }),
        );
      } catch (error) {
        message.error(
          error instanceof Error
            ? error.message
            : $t('dhcp.reservations.messages.releaseFailed'),
        );
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
  confirmText: $t('dhcp.common.save'),
  onConfirm: () => confirmEdit(),
});

function openEdit(row: LedgerRow) {
  editTarget.value = row;
  editForm.address = row.address;
  editForm.mac = row.mac || '';
  editModalApi.setState({
    title: row.subnetId
      ? $t('dhcp.reservations.editBindingTitle', { address: row.address })
      : $t('dhcp.reservations.editMacTitle', { address: row.address }),
  });
  editModalApi.open();
}

async function confirmEdit() {
  const row = editTarget.value;
  if (!row) return;
  const normMac = normalizeMacInput(editForm.mac);
  const addr = editForm.address.trim();
  if (!normMac) {
    message.warning($t('dhcp.reservations.messages.macInvalid'));
    return;
  }
  try {
    if (addr === row.address) {
      await updateBinding(row.address, normMac);
    } else {
      if (!row.subnetId) {
        message.warning($t('dhcp.reservations.messages.needSubnetForAddress'));
        return;
      }
      await releaseAddress(row.address);
      await bindStatic(row.subnetId, addr, normMac);
    }
    message.success(
      $t('dhcp.reservations.messages.bindingUpdated', {
        address: addr,
        mac: normMac,
      }),
    );
    editModalApi.close();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.reservations.messages.updateFailed'),
    );
  } finally {
    await loadLists();
  }
}

</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:pin"
      :title="$t('dhcp.reservations.title')"
      :description="$t('dhcp.reservations.description')"
    >
      <template #actions>
        <Button size="small" @click="loadLists()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1" />{{
            $t('dhcp.common.refresh')
          }}
        </Button>
      </template>
    </PageHeader>

    <Card :title="$t('dhcp.reservations.bulkCard')">
      <template #extra>
        <span class="ipam-hint">{{ $t('dhcp.reservations.bulkExtra') }}</span>
      </template>
      <div class="ipam-toolbar mb-4">
        <span class="ipam-field__label">{{ $t('dhcp.reservations.targetSubnet') }}</span>
        <Select
          v-model:value="selectedSubnet"
          :options="subnetOptions"
          :placeholder="$t('dhcp.reservations.subnetPlaceholder')"
          show-search
          style="width: 320px"
        />
        <Button @click="addRow('reserve')">
          {{ $t('dhcp.reservations.addReserveRow') }}
        </Button>
        <Button @click="addRow('bind')">
          {{ $t('dhcp.reservations.addBindRow') }}
        </Button>
        <Button type="primary" :loading="submitting" @click="submitBulk">
          {{ $t('dhcp.reservations.submitBulk') }}
        </Button>
      </div>
      <div v-for="(r, i) in entries" :key="i" class="mb-2 flex flex-wrap items-center gap-2">
        <Select
          :options="kindOptions"
          :value="r.kind"
          style="width: 130px"
          @change="(v: any) => switchKind(i, v)"
        />
        <Input
          v-model:value="r.address"
          style="width: 200px"
          :placeholder="$t('dhcp.reservations.addressPlaceholder')"
        />
        <Input
          v-if="r.kind === 'bind'"
          v-model:value="r.mac"
          style="width: 180px"
          :placeholder="$t('dhcp.reservations.macPlaceholder')"
        />
        <Input
          v-model:value="r.reason"
          style="width: 180px"
          :placeholder="$t('dhcp.reservations.reasonPlaceholder')"
        />
        <Button size="small" danger @click="removeRow(i)">
          {{ $t('dhcp.common.remove') }}
        </Button>
      </div>
      <div v-if="bulkResult && !bulkResult.ok" class="mt-2 rounded bg-red-50 p-2 text-xs text-red-600">
        {{ $t('dhcp.reservations.rollbackDetail') }}
        <div v-for="f in bulkResult.failures" :key="f.line">
          {{
            $t('dhcp.reservations.rollbackLine', {
              line: f.line,
              reason: f.reason,
            })
          }}
        </div>
      </div>
      <div v-else-if="bulkResult?.ok" class="mt-2 text-xs text-gray-500">
        {{ $t('dhcp.reservations.appliedLines', { n: bulkResult.applied }) }}
      </div>
    </Card>

    <Card :title="$t('dhcp.reservations.listCard')">
      <template #extra>
        <span class="ipam-hint">
          {{ $t('dhcp.reservations.listCount', { n: listRows.length }) }}
        </span>
      </template>
      <div class="ipam-toolbar mb-4">
        <RadioGroup
          v-model:value="familyTab"
          option-type="button"
          size="small"
          :options="[{ label: 'IPv4', value: 'v4' }, { label: 'IPv6', value: 'v6' }]"
        />
        <Input
          v-model:value="search"
          allow-clear
          :placeholder="$t('dhcp.reservations.searchPlaceholder')"
          style="width: 380px"
        />
      </div>
      <ListGrid v-if="familyTab === 'v4'" :table-data="listRows">
        <template #state="{ row }">
          <Tag v-if="row.state === 'static'" color="geekblue">
            {{ $t('dhcp.reservations.stateStatic') }}
          </Tag>
          <Tag v-else color="orange">
            {{ $t('dhcp.reservations.stateReserve') }}
          </Tag>
        </template>
        <template #mac="{ row }">
          <Tag v-if="row.mac" class="font-mono" color="default">{{ row.mac }}</Tag>
          <span v-else>-</span>
        </template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #actions="{ row }">
          <Button v-if="row.state === 'static'" size="small" class="mr-2" @click="openEdit(row)">
            {{ $t('dhcp.common.edit') }}
          </Button>
          <Button size="small" danger @click="releaseRow(row)">
            {{ $t('dhcp.reservations.release') }}
          </Button>
        </template>
      </ListGrid>
      <ListGrid v-else :table-data="listRows">
        <template #state="{ row }">
          <Tag v-if="row.state === 'static'" color="geekblue">
            {{ $t('dhcp.reservations.stateStatic') }}
          </Tag>
          <Tag v-else color="orange">
            {{ $t('dhcp.reservations.stateReserve') }}
          </Tag>
        </template>
        <template #mac="{ row }">
          <Tag v-if="row.mac" class="font-mono" color="default">{{ row.mac }}</Tag>
          <span v-else>-</span>
        </template>
        <template #hostname="{ row }">{{ row.hostname || '-' }}</template>
        <template #actions="{ row }">
          <Button v-if="row.state === 'static'" size="small" class="mr-2" @click="openEdit(row)">
            {{ $t('dhcp.common.edit') }}
          </Button>
          <Button size="small" danger @click="releaseRow(row)">
            {{ $t('dhcp.reservations.release') }}
          </Button>
        </template>
      </ListGrid>
    </Card>

    <EditModal>
      <div class="flex flex-col gap-3">
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-gray-400">
            {{ $t('dhcp.reservations.col.address') }}
          </span>
          <Input
            v-model:value="editForm.address"
            :disabled="!editTarget?.subnetId"
            style="width: 220px"
            :placeholder="$t('dhcp.reservations.addressPlaceholderExample')"
          />
          <span v-if="!editTarget?.subnetId" class="text-xs text-gray-400">
            {{ $t('dhcp.reservations.noSubnetHint') }}
          </span>
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 text-right text-xs text-gray-400">MAC</span>
          <Input
            v-model:value="editForm.mac"
            style="width: 220px"
            :placeholder="$t('dhcp.reservations.macPlaceholder')"
          />
        </div>
        <div class="text-xs text-gray-400">
          {{ $t('dhcp.reservations.editHint') }}
        </div>
      </div>
    </EditModal>
  </div>
</template>
