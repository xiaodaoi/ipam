<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';

import type { VxeGridProps } from '@vben/plugins/vxe-table';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import {
  Button,
  Card,
  Input,
  InputNumber,
  Menu,
  message,
  RadioGroup,
  Select,
  Tag,
} from 'ant-design-vue';

import {
  createSubnet,
  deleteSubnet,
  listOrgTree,
  listSubnets,
  updateSubnet,
  type Subnet,
  type OrgTreeNode,
  listDhcpLeases6,
  type DhcpLease6Row,
} from '#/api/ipam';

const rows = ref<Subnet[]>([]);
const editingId = ref<string>();
const orgTree = ref<OrgTreeNode[]>([]);
const loading = ref(false);
const filterOrgId = ref<string>();

// 新建表单
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '子网信息', confirmText: '创建（下发 Kea）', onConfirm: () => add() });
const form = ref({
  orgId: '',
  name: '',
  family: 4 as 4 | 6,
  cidr: '',
  gateway: '',
  dnsServers: '',
  poolStart: '',
  poolEnd: '',
  poolKind: 'dynamic' as 'dynamic' | 'pd',
  poolPrefixLen: 64,
  poolDelegatedLen: 80,
});

function flattenOrgs(nodes: OrgTreeNode[], depth = 0): { id: string; label: string }[] {
  const out: { id: string; label: string }[] = [];
  for (const n of nodes) {
    out.push({ id: n.id, label: `${'　'.repeat(depth)}${n.name}` });
    if (n.children?.length) out.push(...flattenOrgs(n.children, depth + 1));
  }
  return out;
}
const orgOptions = computed(() => flattenOrgs(orgTree.value));

// ── 组织垂直菜单（子菜单弹出）──
const orgMenuItems = computed(() => {
  const walk = (nodes: OrgTreeNode[]): any[] =>
    nodes.map((n) => ({
      key: n.id,
      label: n.name,
      children: n.children?.length ? walk(n.children) : undefined,
    }));
  return walk(orgTree.value);
});
function onSelectMenu(key: string) {
  filterOrgId.value = key === filterOrgId.value ? undefined : key;
  void load();
}

// ── 分族新建（族由所在页签决定，表单不再选族）──
function openAdd(family: 4 | 6) {
  cancelEdit();
  form.value.family = family;
  formModalApi.setState({ title: `新建 IPv${family} 子网`, confirmText: '创建（下发 Kea）' });
  formModalApi.open();
}

async function load() {
  loading.value = true;
  try {
    const [subs, orgs] = await Promise.all([listSubnets(filterOrgId.value), listOrgTree()]);
    rows.value = subs.items ?? [];
    orgTree.value = orgs;
  } finally {
    loading.value = false;
  }
}
async function add() {
  const f = form.value;
  if (!f.orgId || !f.name || !f.cidr) return;
  let pools:
    | { startAddr: string; endAddr?: string; kind: string; prefixLen?: number; delegatedLen?: number }[]
    | undefined;
  if (f.family === 6 && f.poolKind === 'pd' && f.poolStart && f.poolPrefixLen && f.poolDelegatedLen) {
    // PD 委派池：startAddr=委派前缀，endAddr 由后端推导（M2-018）
    pools = [{ startAddr: f.poolStart, prefixLen: f.poolPrefixLen, delegatedLen: f.poolDelegatedLen, kind: 'pd' }];
  } else if (f.poolStart && f.poolEnd) {
    pools = [{ startAddr: f.poolStart, endAddr: f.poolEnd, kind: f.family === 6 ? f.poolKind : 'dynamic' }];
  }
  if (editingId.value) {
    await updateSubnet(editingId.value, {
      name: f.name, cidr: f.cidr,
      gateway: f.gateway || undefined, dnsServers: f.dnsServers || undefined, pools,
    });
    message.success('子网已更新并下发 Kea');
  } else {
    await createSubnet({
      orgId: f.orgId, name: f.name, family: f.family, cidr: f.cidr, pools,
      gateway: f.gateway || undefined, dnsServers: f.dnsServers || undefined,
    });
  }
  editingId.value = undefined;
  formModalApi.close();
  form.value = { orgId: f.orgId, name: '', family: 4, cidr: '', gateway: '', dnsServers: '', poolStart: '', poolEnd: '', poolKind: 'dynamic', poolPrefixLen: 64, poolDelegatedLen: 80 };
  await load();
}
function cancelEdit() {
  editingId.value = undefined;
  form.value = { orgId: form.value.orgId, name: '', family: 4, cidr: '', gateway: '', dnsServers: '', poolStart: '', poolEnd: '', poolKind: 'dynamic', poolPrefixLen: 64, poolDelegatedLen: 80 };
}
function edit(r: Subnet) {
  const p0 = (r.pools ?? [])[0];
  editingId.value = r.id;
  form.value = {
    orgId: r.orgId, name: r.name, family: (r.family as 4 | 6), cidr: r.cidr,
    gateway: r.gateway ?? '', dnsServers: r.dnsServers ?? '',
    poolStart: p0?.startAddr ?? '', poolEnd: p0?.endAddr ?? '',
    poolKind: (p0?.kind as 'dynamic' | 'pd') ?? 'dynamic',
    poolPrefixLen: p0?.prefixLen ?? 64, poolDelegatedLen: p0?.delegatedLen ?? 80,
  };
  formModalApi.setState({ title: '编辑子网', confirmText: '保存修改' });
  formModalApi.open();
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteSubnet(id);
  } catch (e) {
    // 引用保护/Kea 下发失败：detail 已由 request 层 message 展示
  }
  await load();
}
const lease6Rows = ref<DhcpLease6Row[]>([]);
const lease6Loading = ref(false);
async function loadLeases6() {
  lease6Loading.value = true;
  try {
    lease6Rows.value = (await listDhcpLeases6()).items ?? [];
  } finally {
    lease6Loading.value = false;
  }
}

onMounted(() => {
  load();
  void loadLeases6();
});

const orgName = (id?: string) => {
  const hit = orgOptions.value.find((o) => o.id === id);
  return hit?.label.trim() ?? id ?? '—';
};

// ── IPv4/IPv6 分族（Tabs 分开展示）──
const familyTab = ref('v4');
const v4Rows = computed(() => rows.value.filter((r) => r.family === 4));
const v6Rows = computed(() => rows.value.filter((r) => r.family === 6));

// ── Vben Vxe Table：子网与地址池（操作列固定右侧）──
const gridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'name', title: '名称', minWidth: 120 },
    { field: 'cidr', title: 'CIDR', minWidth: 140 },
    { field: 'orgId', title: '组织', minWidth: 100, slots: { default: 'orgId' } },
    { field: 'gateway', title: '网关/DNS', minWidth: 110, slots: { default: 'gateway' } },
    { field: 'pools', title: '池数', minWidth: 80, slots: { default: 'pools' } },
    { field: 'keaSubnetId', title: 'Kea ID', minWidth: 100, slots: { default: 'keaSubnetId' } },
    { field: 'op', title: '操作', width: 150, fixed: 'right', slots: { default: 'op' } },
  ],
  loading: loading.value,
  rowConfig: { keyField: 'id' },
});
const [SubnetGrid] = useVbenVxeGrid({ gridOptions });

// ── Vben Vxe Table：PD 租约（IPv6 实时查询）──
const lease6GridOptions = reactive<VxeGridProps>({
  columns: [
    { field: 'ipAddress', title: '地址/前缀', minWidth: 160 },
    { field: 'leaseType', title: '类型', width: 100 },
    { field: 'prefixLen', title: '前缀长', width: 90 },
    { field: 'duid', title: 'DUID', minWidth: 180 },
    { field: 'hwAddress', title: 'MAC', minWidth: 140 },
    { field: 'validLifetime', title: '有效期(s)', width: 110 },
  ],
  loading: lease6Loading.value,
  rowConfig: { keyField: 'ipAddress' },
});
const [Lease6Grid] = useVbenVxeGrid({ gridOptions: lease6GridOptions });
</script>

<template>
  <div class="p-4">
  <div class="flex gap-4">
    <!-- 左侧组织垂直菜单（紧凑窄栏） -->
    <Card title="组织" class="w-36 shrink-0 self-start" :body-style="{ padding: '2px 0' }">
      <Menu
        class="org-menu max-h-[460px] overflow-auto"
        mode="vertical"
        :items="orgMenuItems"
        :selected-keys="filterOrgId ? [filterOrgId] : []"
        :inline-indent="10"
        @click="({ key }: any) => onSelectMenu(String(key))"
      />
      <div class="px-2 pb-1 pt-2">
        <Button size="small" block @click="filterOrgId = undefined; load()">全部</Button>
      </div>
    </Card>

    <div class="min-w-0 flex-1">
    <Card>
      <template #title>
        <span>子网与地址池</span>
      </template>

      <FormModal class="w-[860px]">
        <div class="flex flex-wrap items-end gap-2">
        <div>
          <div class="mb-1 text-xs text-gray-400">组织</div>
          <Select v-model:value="form.orgId" style="width: 180px" placeholder="选择组织节点"
            :options="orgOptions.map((o) => ({ value: o.id, label: o.label }))" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">名称</div>
          <Input v-model:value="form.name" style="width: 150px" placeholder="研发-办公" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">{{ form.family === 6 ? 'DNS 服务器' : '网关' }}</div>
          <Input v-model:value="form.gateway" v-if="form.family === 4" style="width: 140px" placeholder="10.61.172.1" />
          <Input v-model:value="form.dnsServers" v-if="form.family === 6" style="width: 170px" placeholder="2406:172::53" />
        </div>
        <div v-if="form.family === 4">
          <div class="mb-1 text-xs text-gray-400">DNS 服务器</div>
          <Input v-model:value="form.dnsServers" style="width: 170px" placeholder="223.5.5.5, 114.114.114.114" />
        </div>
        <template v-if="form.family === 6">
          <div>
            <div class="mb-1 text-xs text-gray-400">池类型</div>
            <Select v-model:value="form.poolKind" style="width: 110px" :options="[{ value: 'dynamic', label: '地址池' }, { value: 'pd', label: 'PD 委派' }]" />
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">{{ form.poolKind === 'pd' ? '委派前缀' : '池起' }}</div>
            <Input v-model:value="form.poolStart" style="width: 170px" :placeholder="form.poolKind === 'pd' ? '2001:db8:1::' : '2406:172::100'" />
          </div>
          <template v-if="form.poolKind === 'pd'">
            <div>
              <div class="mb-1 text-xs text-gray-400">前缀长度</div>
              <InputNumber v-model:value="form.poolPrefixLen" :min="1" :max="128" style="width: 80px" />
            </div>
            <div>
              <div class="mb-1 text-xs text-gray-400">委派长度</div>
              <InputNumber v-model:value="form.poolDelegatedLen" :min="1" :max="128" style="width: 80px" />
            </div>
          </template>
          <div v-if="form.poolKind === 'dynamic'">
            <div class="mb-1 text-xs text-gray-400">池止</div>
            <Input v-model:value="form.poolEnd" style="width: 140px" placeholder="2406:172::200" />
          </div>
        </template>
        <template v-else>
          <div>
            <div class="mb-1 text-xs text-gray-400">池起</div>
            <Input v-model:value="form.poolStart" style="width: 140px" placeholder="10.61.172.100" />
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">池止</div>
            <Input v-model:value="form.poolEnd" style="width: 140px" placeholder="10.61.172.200" />
          </div>
        </template>
        </div>
      </FormModal>

      <div class="mb-3 flex items-center justify-between">
        <RadioGroup
          v-model:value="familyTab"
          option-type="button"
          size="small"
          :options="[{ label: 'IPv4', value: 'v4' }, { label: 'IPv6', value: 'v6' }]"
        />
        <Button size="small" type="primary" @click="openAdd(familyTab === 'v6' ? 6 : 4)">
          + 新建 {{ familyTab === 'v6' ? 'IPv6' : 'IPv4' }} 子网
        </Button>
      </div>

      <template v-if="familyTab === 'v4'">
        <SubnetGrid :table-data="v4Rows">
          <template #orgId="{ row }">
            {{ orgName(row.orgId) }}
          </template>
          <template #gateway="{ row }">
            {{ row.gateway || '-' }}
          </template>
          <template #pools="{ row }">
            {{ (row.pools ?? []).length }}
          </template>
          <template #keaSubnetId="{ row }">
            <Tag v-if="row.keaSubnetId" color="green">{{ row.keaSubnetId }}</Tag>
            <Tag v-else color="orange">未下发</Tag>
          </template>
          <template #op="{ row }">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(row as Subnet)">编辑</Button>
              <Button size="small" danger @click="remove(row.id)">删除</Button>
            </div>
          </template>
        </SubnetGrid>
      </template>
      <template v-else>
        <SubnetGrid :table-data="v6Rows">
          <template #orgId="{ row }">
            {{ orgName(row.orgId) }}
          </template>
          <template #gateway="{ row }">
            {{ row.gateway || row.dnsServers || '-' }}
          </template>
          <template #pools="{ row }">
            {{ (row.pools ?? []).length }}
          </template>
          <template #keaSubnetId="{ row }">
            <Tag v-if="row.keaSubnetId" color="green">{{ row.keaSubnetId }}</Tag>
            <Tag v-else color="orange">未下发</Tag>
          </template>
          <template #op="{ row }">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(row as Subnet)">编辑</Button>
              <Button size="small" danger @click="remove(row.id)">删除</Button>
            </div>
          </template>
        </SubnetGrid>
        <div class="mt-5 mb-2 flex items-center justify-between">
          <span class="text-sm font-medium">PD 租约（DHCPv6 · Kea 实时查询）</span>
          <Button size="small" :loading="lease6Loading" @click="loadLeases6">刷新</Button>
        </div>
        <Lease6Grid :table-data="lease6Rows" />
      </template>
      </Card>
      </div>
  </div>
  </div>
</template>
<style scoped>
.org-menu :deep(.ant-menu-item),
.org-menu :deep(.ant-menu-submenu-title) {
  height: 32px;
  line-height: 32px;
  font-size: 13px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.org-menu :deep(.ant-menu) {
  border-inline-end: none;
}
</style>
