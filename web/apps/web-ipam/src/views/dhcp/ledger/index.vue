<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import {
  Card,
  Input,
  Menu,
  message,
  Table,
} from 'ant-design-vue';

import IpPlanMap from '#/components/ip-plan-map.vue';

import {
  bindStatic,
  bulkReservations,
  listLedger,
  listOrgTree,
  listSubnets,
  releaseAddress,
  reserveAddress,
  type OrgTreeNode,
  type Subnet,
} from '#/api/ipam';
import { normalizeMacInput } from '#/utils/mac';

// ── IP 工具（与 IpPlanMap 同源）──
function ipToInt(ip: string): number {
  return ip.split('.').reduce((acc, o) => ((acc << 8) >>> 0) + parseInt(o, 10), 0) >>> 0;
}
function intToIp(n: number): string {
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.');
}
function parseCidr(cidr: string): { network: number; broadcast: number; hostCount: number } {
  const parts = String(cidr).split('/');
  const bits = parseInt(parts[1] || '32', 10);
  const ipInt = ipToInt(parts[0] || '0.0.0.0');
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  const network = (ipInt & mask) >>> 0;
  const broadcast = (network | (~mask >>> 0)) >>> 0;
  return { network, broadcast, hostCount: broadcast - network + 1 };
}

// ── 组织树（垂直菜单，子菜单弹出）──
const orgTree = ref<OrgTreeNode[]>([]);
const orgMenuItems = computed(() => {
  const walk = (nodes: OrgTreeNode[]): any[] =>
    nodes.map((n) => ({
      key: n.id,
      label: n.name,
      children: n.children?.length ? walk(n.children) : undefined,
    }));
  return walk(orgTree.value);
});
const selectedOrgId = ref<string>('');

function onSelectMenu(key: string) {
  selectedOrgId.value = key === selectedOrgId.value ? '' : key;
  selectedCidr.value = '';
  void loadSubnets();
}

// ── 子网（v4/v6 分栏）──
const subnets = ref<Subnet[]>([]);
const v4Subnets = computed(() => subnets.value.filter((s) => s.family === 4));
const v6Subnets = computed(() => subnets.value.filter((s) => s.family === 6));
const selectedCidr = ref<string>('');

async function loadSubnets() {
  subnets.value = (await listSubnets(selectedOrgId.value || undefined)).items ?? [];
  if (v4Subnets.value.length) {
    const cur = v4Subnets.value.find((s) => s.cidr === selectedCidr.value);
    selectedCidr.value = (cur ?? v4Subnets.value[0]!).cidr;
    await loadMap();
  } else {
    cells.value = [];
    selectedCidr.value = '';
  }
}

// ── 地址地图数据（IPv4 逐地址）──
interface MapCell {
  ip: string;
  host: number;
  status: string;
  overlays?: string[];
  hostname?: string;
  user?: string;
  leaseEnd?: string;
  purpose?: string;
  remark?: string;
  leaseStatus?: string;
}
const cells = ref<MapCell[]>([]);
const mapLoading = ref(false);
const currentSubnet = computed(() => v4Subnets.value.find((s) => s.cidr === selectedCidr.value));

/** ledger 状态 → 地图状态/叠加 */
function mapState(state: string): { status: string; overlays: string[] } {
  switch (state) {
    case 'online': return { status: 'dynamic', overlays: ['online'] };
    case 'grace': return { status: 'dynamic', overlays: [] };
    case 'conflict': return { status: 'dynamic', overlays: ['conflict'] };
    default: return { status: state, overlays: [] }; // available/static/reserved 直通
  }
}

async function loadMap() {
  const sub = currentSubnet.value;
  if (!sub) {
    cells.value = [];
    return;
  }
  mapLoading.value = true;
  try {
    const { network, hostCount } = parseCidr(sub.cidr);
    // 1) 生成全量格子（网络/广播/未规划）
    const grid: MapCell[] = [];
    for (let n = 0; n < hostCount; n++) {
      const ip = intToIp(network + n);
      grid.push({
        ip,
        host: n,
        status: n === 0 ? 'network' : n === hostCount - 1 ? 'broadcast' : 'available',
        overlays: [],
      });
    }
    // 2) 覆盖台账状态（按 host 定位）
    const page = await listLedger({ subnetId: sub.id, family: 4, pageSize: 500 });
    for (const row of page.items ?? []) {
      const host = ipToInt(row.address) - network;
      if (host < 0 || host >= hostCount) continue;
      const { status, overlays } = mapState(row.state);
      grid[host] = {
        ip: row.address,
        host,
        status,
        overlays,
        hostname: row.hostname || '',
        user: row.owner || '',
        leaseEnd: row.leaseExpiry ? new Date(row.leaseExpiry).toLocaleString() : '',
        leaseStatus: row.state === 'online' ? '已分配' : '',
        purpose: row.state === 'reserved' ? '保留' : '',
        remark: row.state === 'conflict' ? 'IP 冲突' : row.state === 'grace' ? '租约宽限' : '',
      };
    }
    cells.value = grid;
  } finally {
    mapLoading.value = false;
  }
}

function onSubnetChange(cidr: string) {
  selectedCidr.value = cidr;
  void loadMap();
}

// ── 地图操作 → 台账 API ──
const bindModal = ref({ address: '', subnetId: '', mac: '' });
const [BindModal, bindModalApi] = useVbenModal({ draggable: true, confirmText: '绑定', onConfirm: () => confirmBind() });

async function onMapAction(action: string, ips: MapCell[]) {
  const sub = currentSubnet.value;
  const first = ips[0];
  if (!sub || !first) return;
  if (action === 'toStatic') {
    bindModal.value = { address: first.ip, subnetId: sub.id, mac: '' };
    bindModalApi.setState({ title: `静态绑定 ${first.ip}` });
    bindModalApi.open();
    return;
  }
  try {
    if (action === 'toReserve') {
      if (ips.length === 1) {
        await reserveAddress(sub.id, ips[0]!.ip);
      } else {
        const res = await bulkReservations({
          subnetId: sub.id,
          entries: ips.map((c) => ({ address: c.ip, kind: 'reserve' })),
        });
        if (!res.ok) {
          const reasons = (res.failures ?? []).map((f) => f.reason).join('；');
          throw new Error(reasons || '存在失败行，整体已回滚');
        }
      }
      message.success(`已保留 ${ips.length} 个地址`);
    } else if (action === 'toRelease') {
      const failed: string[] = [];
      for (const c of ips) {
        try {
          await releaseAddress(c.ip);
        } catch {
          failed.push(c.ip);
        }
      }
      if (failed.length) {
        message.warning(`已释放 ${ips.length - failed.length} 个，失败（无预留记录）：${failed.join('、')}`);
      } else {
        message.success(`已释放 ${ips.length} 个地址，回归可下发池`);
      }
    }
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  void loadMap();
}
async function confirmBind() {
  const normMac = normalizeMacInput(bindModal.value.mac);
  if (!normMac || !bindModal.value.subnetId) {
    message.warning(
      '请填写合法 MAC（支持 C4-3D-1A-07-EB-2B / C43D1A07EB2B / 冒号分隔，大小写均可）',
    );
    return;
  }
  try {
    await bindStatic(bindModal.value.subnetId, bindModal.value.address, normMac);
    message.success(`${bindModal.value.address} 已静态绑定 ${normMac}`);
    bindModalApi.close();
    void loadMap();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '绑定失败');
  }
}

// ── IPv6 网段表格 ──
const v6Cols = [
  { title: '网段', dataIndex: 'cidr' },
  { title: '名称', dataIndex: 'name' },
  { title: '池', dataIndex: 'pools' },
];
function poolText(p: Subnet): string {
  return (p.pools ?? [])
    .map((x: any) => (x.kind === 'pd' ? `PD:${x.startAddr}/${x.prefixLen}→${x.delegatedLen}` : `${x.startAddr}-${x.endAddr ?? ''}`))
    .join('；') || '—';
}

onMounted(async () => {
  orgTree.value = await listOrgTree();
  await loadSubnets();
});
</script>

<template>
  <div class="p-4">
  <div class="flex gap-4">
    <Card title="组织" class="w-36 shrink-0 self-start" :body-style="{ padding: '2px 0' }">
      <Menu
        class="org-menu max-h-[460px] overflow-auto"
        mode="vertical"
        :items="orgMenuItems"
        :selected-keys="selectedOrgId ? [selectedOrgId] : []"
        :inline-indent="10"
        @click="({ key }: any) => onSelectMenu(String(key))"
      />
      <div class="px-2 pb-1 pt-2 text-xs text-gray-400">点击选择组织（再点取消）</div>
    </Card>

    <div class="min-w-0 flex-1">
      <div v-if="!v4Subnets.length && !v6Subnets.length" class="rounded border border-dashed py-16 text-center text-gray-400">
        请先在左侧选择组织；或该组织暂无网段
      </div>

      <!-- IPv4：地址地图（独立 Card，还原 ip-plan-map 原始布局） -->
      <IpPlanMap
        v-if="v4Subnets.length"
        style="max-width: 980px"
        :cidr="selectedCidr"
        :ips="cells"
        :subnets="v4Subnets.map((s) => ({ cidr: s.cidr, name: s.name }))"
        @subnet-change="onSubnetChange"
        @action="onMapAction"
        @save="(_t: unknown, _v: unknown) => message.info('台账信息由 DHCP 租约 / 资产登记驱动，此处仅展示')"
      />

      <!-- IPv6：网段表格 -->
      <Card v-if="v6Subnets.length" title="IPv6 网段" size="small" class="mt-4">
        <template #extra>
          <span class="text-xs text-gray-400">IPv6 为子网级汇总，暂无逐地址地图</span>
        </template>
        <Table
          :data-source="v6Subnets"
          :columns="v6Cols"
          row-key="id"
          size="small"
          :pagination="false"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'pools'">{{ poolText(record as Subnet) }}</template>
          </template>
        </Table>
      </Card>
    </div>
  </div>

  <BindModal>
    <Input v-model:value="bindModal.mac" placeholder="MAC 如 aa:bb:cc:dd:ee:01" @pressEnter="confirmBind" />
  </BindModal>
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
