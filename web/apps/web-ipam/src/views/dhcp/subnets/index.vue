<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import { Button, Card, Input, Select, Table, Tag, Tree } from 'ant-design-vue';

import {
  createSubnet,
  deleteSubnet,
  listOrgTree,
  listSubnets,
  type Subnet,
  type OrgTreeNode,
} from '#/api/ipam';

const rows = ref<Subnet[]>([]);
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
  poolStart: '',
  poolEnd: '',
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
  const pools =
    f.poolStart && f.poolEnd
      ? [{ startAddr: f.poolStart, endAddr: f.poolEnd, kind: 'dynamic' }]
      : undefined;
  await createSubnet({
    orgId: f.orgId, name: f.name, family: f.family, cidr: f.cidr, pools,
  });
  showForm.value = false;
  form.value = { orgId: f.orgId, name: '', family: 4, cidr: '', poolStart: '', poolEnd: '' };
  await load();
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
onMounted(load);

const orgName = (id?: string) => {
  const hit = orgOptions.value.find((o) => o.id === id);
  return hit?.label.trim() ?? id ?? '—';
};
const columns = [
  { title: '名称', dataIndex: 'name' },
  { title: 'CIDR', dataIndex: 'cidr' },
  { title: '族', dataIndex: 'family', width: 60 },
  { title: '组织', dataIndex: 'orgId', width: 160 },
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
          <Button size="small" type="primary" @click="showForm = !showForm">{{ showForm ? '收起' : '+ 新建子网' }}</Button>
        </div>
      </template>

      <div v-if="showForm" class="mb-4 flex flex-wrap items-end gap-2 rounded border border-gray-200 p-3">
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
          <Select v-model:value="form.family" style="width: 80px" :options="[{ value: 4, label: 'IPv4' }, { value: 6, label: 'IPv6' }]" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">CIDR</div>
          <Input v-model:value="form.cidr" style="width: 180px" placeholder="10.61.172.0/24" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">池起</div>
          <Input v-model:value="form.poolStart" style="width: 140px" placeholder="10.61.172.100" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">池止</div>
          <Input v-model:value="form.poolEnd" style="width: 140px" placeholder="10.61.172.200" />
        </div>
        <Button type="primary" @click="add">创建（下发 Kea）</Button>
      </div>

      <Table
        :data-source="rows"
        :columns="columns"
        row-key="id"
        size="small"
        :loading="loading"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
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
            <Button size="small" danger @click="remove(record.id)">删除</Button>
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>