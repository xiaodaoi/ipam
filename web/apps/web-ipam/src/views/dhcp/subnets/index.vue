<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import { VbenModal } from '@vben/common-ui';

import { Button, Card, Input, InputNumber, message, Select, Table, Tag, Tree } from 'ant-design-vue';

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
const showForm = ref(false);
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
  showForm.value = false;
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
  showForm.value = true;
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
const lease6Cols = [
  { title: '地址/前缀', dataIndex: 'ipAddress' },
  { title: '类型', dataIndex: 'leaseType', width: 90 },
  { title: '前缀长', dataIndex: 'prefixLen', width: 80 },
  { title: 'DUID', dataIndex: 'duid' },
  { title: 'MAC', dataIndex: 'hwAddress' },
  { title: '有效期(s)', dataIndex: 'validLifetime', width: 100 },
];

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
const columns = [
  { title: '名称', dataIndex: 'name' },
  { title: 'CIDR', dataIndex: 'cidr' },
  { title: '族', dataIndex: 'family', width: 60 },
  { title: '组织', dataIndex: 'orgId', width: 160 },
  { title: '网关', key: 'gateway', width: 120 },
  { title: '池数', key: 'pools', width: 70 },
  { title: 'Kea ID', dataIndex: 'keaSubnetId', width: 90 },
  { title: '操作', key: 'op', width: 80 },
];
</script>

<template>
  <div class="flex gap-4">
    <!-- 左侧组织树 -->
    <Card title="组织" class="w-64 shrink-0">
      <Tree
        :tree-data="orgTree.map((n) => ({ key: n.id, title: n.name, children: (n.children ?? []).map((c) => ({ key: c.id, title: c.name })) }))"
        :selected-keys="filterOrgId ? [filterOrgId] : []"
        :default-expand-all="true"
        @select="(keys: any) => { filterOrgId = keys[0] as string; load(); }"
      />
      <Button size="small" block class="mt-2" @click="filterOrgId = undefined; load()">全部</Button>
    </Card>

    <Card class="flex-1">
      <template #title>
        <div class="flex items-center gap-3">
          <span>子网与地址池</span>
          <Button size="small" type="primary" @click="editingId = undefined; cancelEdit(); showForm = true">+ 新建子网</Button>
        </div>
      </template>

      <VbenModal v-model:open="showForm" :title="editingId ? '编辑子网' : '新建子网'" draggable>
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
          <div class="mb-1 text-xs text-gray-400">族</div>
          <Select v-model:value="form.family" :disabled="!!editingId" style="width: 80px" :options="[{ value: 4, label: 'IPv4' }, { value: 6, label: 'IPv6' }]" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">CIDR</div>
          <Input v-model:value="form.cidr" style="width: 180px" placeholder="10.61.172.0/24" />
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
        <Button type="primary" @click="add">{{ editingId ? '保存修改' : '创建（下发 Kea）' }}</Button>
        </div>
      </VbenModal>

      <Table
        :data-source="rows"
        :columns="columns"
        row-key="id"
        size="small"
        :loading="loading"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'gateway'">{{ record.gateway || '-' }}</template>
          <template v-if="column.dataIndex === 'family'">
            <Tag :color="record.family === 4 ? 'blue' : 'purple'">v{{ record.family }}</Tag>
          </template>
          <template v-else-if="column.dataIndex === 'orgId'">
            {{ orgName(record.orgId) }}
          </template>
          <template v-else-if="column.key === 'pools'">
            {{ (record.pools ?? []).length }}
          </template>
          <template v-else-if="column.dataIndex === 'keaSubnetId'">
            <Tag v-if="record.keaSubnetId" color="green">{{ record.keaSubnetId }}</Tag>
            <Tag v-else color="orange">未下发</Tag>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" class="mr-1" @click="edit(record as Subnet)">编辑</Button>
            <Button size="small" danger @click="remove(record.id)">删除</Button>
          </template>
        </template>
      </Table>
    </Card>
    <Card title="PD 租约（DHCPv6 · Kea 实时查询）" class="mt-3">
      <template #extra>
        <Button size="small" :loading="lease6Loading" @click="loadLeases6">刷新</Button>
      </template>
      <Table :data-source="lease6Rows" :columns="lease6Cols" :loading="lease6Loading" size="small" row-key="ipAddress" />
    </Card>
  </div>
</template>