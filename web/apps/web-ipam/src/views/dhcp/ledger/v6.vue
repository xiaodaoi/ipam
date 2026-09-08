<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import { Card, Table, Tag } from 'ant-design-vue';

import OrgFilterCard from '#/components/org-filter-card.vue';

import {
  listLedger,
  listOrgTree,
  listSubnets,
  type OrgTreeNode,
  type Subnet,
} from '#/api/ipam';

// ── 组织筛选 ──
const orgTree = ref<OrgTreeNode[]>([]);
const selectedOrgId = ref<string>('');

function onSelectOrg(orgId: string) {
  selectedOrgId.value = orgId;
  void loadSubnets();
}

// ── IPv6 子网（台账视角）──
const subnets = ref<Subnet[]>([]);
const v6Subnets = computed(() => subnets.value.filter((s) => s.family === 6));
const selectedCidr = ref('');
const selectedSubnetId = computed(
  () => v6Subnets.value.find((s) => s.cidr === selectedCidr.value)?.id ?? '',
);
const allOnline = ref<Record<string, any>[]>([]);
const onlineCounts = ref<Record<string, number>>({});
const onlineRows = computed(() =>
  allOnline.value.filter((r) => r.subnetId === selectedSubnetId.value),
);

async function loadOnline() {
  try {
    const d = await listLedger({ family: 6, state: 'online', pageSize: 500 });
    allOnline.value = d.items ?? [];
  } catch {
    allOnline.value = [];
  }
  const cnt: Record<string, number> = {};
  for (const r of allOnline.value) {
    if (r.subnetId) cnt[r.subnetId] = (cnt[r.subnetId] ?? 0) + 1;
  }
  onlineCounts.value = cnt;
  // 未选中或选中网段已不在当前列表时自动回退：优先有在线地址的网段，否则取第一个
  const list = v6Subnets.value;
  if (list.length > 0 && !list.some((s) => s.cidr === selectedCidr.value)) {
    const hit = list.find((s) => (cnt[s.id] ?? 0) > 0);
    selectedCidr.value = (hit ?? list[0])!.cidr;
  }
}

function onSelectSubnet(cidr: string) {
  selectedCidr.value = cidr;
}

async function loadSubnets() {
  subnets.value = (await listSubnets(selectedOrgId.value || undefined)).items ?? [];
  await loadOnline();
}

function fmtTime(v?: string): string {
  return v ? new Date(v).toLocaleString() : '—';
}
const v6Cols = [
  { title: '网段（CIDR）', dataIndex: 'cidr' },
  { title: '名称', dataIndex: 'name' },
  { title: '类型', dataIndex: 'kind' },
  { title: '池范围', dataIndex: 'pools' },
  { title: '在线地址数', dataIndex: 'online', width: 110 },
];
function poolText(p: Subnet): string {
  return (
    (p.pools ?? [])
      .map((x: any) =>
        x.kind === 'pd'
          ? `PD:${x.startAddr}/${x.prefixLen}→${x.delegatedLen}`
          : `${x.startAddr}-${x.endAddr ?? ''}`,
      )
      .join('；') || '—'
  );
}
function kindText(p: Subnet): string {
  const kinds = new Set((p.pools ?? []).map((x: any) => x.kind));
  if (kinds.has('pd')) return 'PD 委派';
  if (kinds.has('dynamic')) return '地址池';
  return '—';
}

onMounted(async () => {
  orgTree.value = await listOrgTree();
  await loadSubnets();
});
</script>

<template>
  <div class="p-4">
  <div class="flex gap-4">
    <OrgFilterCard
      :org-tree="orgTree"
      :selected-org-id="selectedOrgId"
      @select="onSelectOrg"
    />

    <Card class="min-w-0 flex-1" title="IPv6 地址台账（子网级汇总）">
      <template #extra>
        <span class="text-xs text-muted-foreground">
          IPv6 地址由 PD 委派/地址池动态分配，台账按子网级汇总展示
        </span>
      </template>
      <div v-if="!v6Subnets.length" class="rounded border border-dashed py-16 text-center text-muted-foreground">
        请先在左侧选择组织；或该组织暂无 IPv6 网段
      </div>
      <Table
        v-else
        :data-source="v6Subnets"
        :columns="v6Cols"
        row-key="id"
        :pagination="false"
        :row-class-name="(r: any) => (r.cidr === selectedCidr ? 'bg-accent' : '')"
        :custom-row="(r: any) => ({ onClick: () => onSelectSubnet(r.cidr), style: { cursor: 'pointer' } })"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'kind'">{{ kindText(record as Subnet) }}</template>
          <template v-else-if="column.dataIndex === 'pools'">{{ poolText(record as Subnet) }}</template>
          <template v-else-if="column.dataIndex === 'online'">
            <Tag :color="(onlineCounts[(record as Subnet).id] ?? 0) > 0 ? 'green' : 'default'" class="m-0">
              {{ onlineCounts[(record as Subnet).id] ?? 0 }}
            </Tag>
          </template>
        </template>
      </Table>

      <Card class="mt-4" size="small" :title="`在线地址 · ${selectedCidr || '未选择网段'}（${onlineRows.length}）`">
        <template #extra>
          <span class="text-xs text-muted-foreground">点击上方网段行切换查看；客户端标识为 DHCPv6 DUID</span>
        </template>
        <Table
          :data-source="onlineRows"
          :columns="[
            { title: '在线地址', dataIndex: 'address' },
            { title: '客户端标识（DUID）', dataIndex: 'mac' },
            { title: '主机名', dataIndex: 'hostname' },
            { title: '租期到期', dataIndex: 'leaseExpiry' },
          ]"
          row-key="address"
          size="small"
          :pagination="{ pageSize: 20, showSizeChanger: true }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'leaseExpiry'">{{ fmtTime((record as any).leaseExpiry) }}</template>
            <template v-else-if="column.dataIndex === 'mac'">
              <code class="text-xs">{{ (record as any).mac ?? '—' }}</code>
            </template>
          </template>
        </Table>
      </Card>
    </Card>
  </div>
  </div>
</template>
