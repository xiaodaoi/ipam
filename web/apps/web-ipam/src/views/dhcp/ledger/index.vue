<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import { Card, Tag, message, Table, Tree } from 'ant-design-vue';

import {
  bindStatic,
  listLedger,
  listOrgTree,
  reserveAddress,
  type LedgerRow,
  type OrgTreeNode,
} from '#/api/ipam';

const STATE_COLOR: Record<string, string> = {
  online: 'green',
  available: 'default',
  reserved: 'orange',
  static: 'blue',
  grace: 'gold',
  conflict: 'red',
};
const STATE_TEXT: Record<string, string> = {
  online: '在线',
  available: '空闲',
  reserved: '保留',
  static: '静态绑定',
  grace: '宽限',
  conflict: '冲突',
};

const orgTree = ref<OrgTreeNode[]>([]);

// antdv Tree 数据适配（key=节点 id）
const treeData = computed(() => {
  const walk = (nodes: OrgTreeNode[]): Array<{ key: string; title: string; children: any[] }> =>
    nodes.map((n) => ({ key: n.id, title: n.name, children: walk(n.children ?? []) }));
  return walk(orgTree.value);
});
const selectedOrgId = ref<string>('');
const rows = ref<LedgerRow[]>([]);
const total = ref(0);
const loading = ref(false);
const pageSize = ref(50);

const columns = [
  { title: '地址', dataIndex: 'address', key: 'address', width: 180 },
  { title: '状态', dataIndex: 'state', key: 'state', width: 110 },
  { title: 'MAC', dataIndex: 'mac', key: 'mac', width: 180 },
  { title: '主机名', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '使用人', dataIndex: 'owner', key: 'owner', width: 120 },
  { title: '租约到期', dataIndex: 'leaseExpiry', key: 'leaseExpiry' },
  { title: '操作', key: 'actions', width: 160 },
];

onMounted(async () => {
  orgTree.value = await listOrgTree();
  await load();
});

const load = async () => {
  loading.value = true;
  try {
    const page = await listLedger({ orgId: selectedOrgId.value || undefined, pageSize: pageSize.value });
    rows.value = page.items;
    total.value = page.total ?? 0;
  } finally {
    loading.value = false;
  }
};

const onSelectOrg = (_keys: Array<string | number>, info: { selected: boolean }) => {
  selectedOrgId.value = info.selected ? String(_keys[0] ?? '') : '';
  load();
};

const stateColor = (state: string) => STATE_COLOR[state] ?? 'default';
const stateText = (state: string) => STATE_TEXT[state] ?? state;

const actionReserve = async (row: LedgerRow) => {
  if (!row.subnetId) return;
  await reserveAddress(row.subnetId, row.address);
  message.success(`${row.address} 已保留`);
  load();
};

const bindModal = ref({ address: '', subnetId: '', mac: '' });
const [BindModal, bindModalApi] = useVbenModal({ draggable: true, confirmText: '绑定', onConfirm: () => confirmBind() });
function askBind(row: LedgerRow) {
  bindModal.value = { address: row.address, subnetId: row.subnetId ?? '', mac: '' };
  bindModalApi.setState({ title: `绑定 ${row.address}` });
  bindModalApi.open();
}
async function confirmBind() {
  const mac = bindModal.value.mac.trim();
  if (!mac || !bindModal.value.subnetId) return;
  await bindStatic(bindModal.value.subnetId, bindModal.value.address, mac);
  message.success(`${bindModal.value.address} 已静态绑定 ${mac}`);
  bindModalApi.close();
  load();
}
</script>

<template>
  <div>
  <div style="display:flex;gap:16px;height:100%">
    <Card style="width:280px;flex-shrink:0" title="组织分组">
      <Tree
        :tree-data="treeData"
        selectable
        block-node
        @select="onSelectOrg"
      />
    </Card>
    <Card style="flex:1" title="地址台账（六态矩阵）">
      <Table
        :columns="columns"
        :data-source="rows"
        :loading="loading"
        :pagination="false"
        row-key="poolIndex"
        size="middle"
      >
        <template #bodyCell="{ column, record }">
          <!-- record 为通用记录类型，此处按台账行处理 -->
          <template v-if="column.key === 'state'">
            <Tag :color="stateColor((record as LedgerRow).state)">{{ stateText((record as LedgerRow).state) }}</Tag>
          </template>
          <template v-else-if="column.key === 'actions'">
            <a style="margin-right:8px" @click="actionReserve(record as LedgerRow)">保留</a>
            <a style="margin-right:8px" @click="askBind(record as LedgerRow)">静态绑定</a>
          </template>
        </template>
      </Table>
      <div style="margin-top:12px;text-align:right">共 {{ total }} 条</div>
    </Card>
  </div>
  <BindModal>
    <Input v-model:value="bindModal.mac" placeholder="MAC 如 aa:bb:cc:dd:ee:01" @pressEnter="confirmBind" />
  </BindModal>
  </div>
</template>