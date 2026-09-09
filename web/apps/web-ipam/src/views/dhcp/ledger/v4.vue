<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';
import { useRoute } from 'vue-router';

import { Card, Table, Input, message } from 'ant-design-vue';

const showTotal = (t: number) => `共 ${t} 条`;

import IpPlanMap from '#/components/ip-plan-map.vue';
import OrgFilterCard from '#/components/org-filter-card.vue';

import {
  bindStatic,
  listLedger,
  listOrgTree,
  listSubnets,
  releaseAddress,
  reserveAddress,
  type OrgTreeNode,
  type Subnet,
  type LedgerRow,
} from '#/api/ipam';
import { normalizeMacInput } from '#/utils/mac';

// ── 组织筛选 ──
const orgTree = ref<OrgTreeNode[]>([]);
const selectedOrgId = ref<string>('');

function onSelectOrg(orgId: string) {
  selectedOrgId.value = orgId;
  selectedCidr.value = '';
  void loadSubnets();
}

// ── 子网（IPv4）──
const subnets = ref<Subnet[]>([]);
const v4Subnets = computed(() => subnets.value.filter((s) => s.family === 4));
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
  mac?: string;
  user?: string;
  leaseStart?: string;
  leaseEnd?: string;
  purpose?: string;
  remark?: string;
  leaseStatus?: string;
}
const cells = ref<MapCell[]>([]);
const onlineRows = ref<Partial<LedgerRow>[]>([]);
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
    onlineRows.value = (page.items ?? []).filter((r) => r.state === 'online');
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
        mac: row.mac || '',
        user: row.owner || '',
        leaseStart: row.leaseStart ? new Date(row.leaseStart).toLocaleString() : '',
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
        const res = await (await import('#/api/ipam')).bulkReservations({
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
        message.warning(`已释放 ${ips.length - failed.length} 个，失败（无保留记录）：${failed.join('、')}`);
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

const route = useRoute();

onMounted(async () => {
  orgTree.value = await listOrgTree();
  await loadSubnets();
  // 支持子网管理页 CIDR 点击跳转（?cidr=x.x.x.x/nn）
  const want = route.query.cidr;
  if (want && v4Subnets.value.some((s) => s.cidr === String(want))) {
    selectedCidr.value = String(want);
    await loadMap();
  }
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

    <div class="min-w-0 flex-1">
      <IpPlanMap
        v-if="v4Subnets.length"
        :cidr="selectedCidr"
        :ips="cells"
        :subnets="v4Subnets.map((s) => ({ cidr: s.cidr, name: s.name }))"
        @subnet-change="onSubnetChange"
        @action="onMapAction"
        @save="(_t: unknown, _v: unknown) => message.info('台账信息由 DHCP 租约 / 资产登记驱动，此处仅展示')"
      />

      <div v-if="!v4Subnets.length" class="rounded border border-dashed py-16 text-center text-gray-400">
        请先在左侧选择组织；或该组织暂无 IPv4 网段
      </div>

      <Card class="mt-4" :title="`在线地址 · ${selectedCidr || '未选择网段'}（${onlineRows.length}）`">
        <template #extra>
          <span class="text-xs text-muted-foreground">DHCP 租约活跃的地址，随租约实时更新</span>
        </template>
        <Table
          :data-source="onlineRows"
          :columns="[
            { title: '在线地址', dataIndex: 'address' },
            { title: 'MAC', dataIndex: 'mac' },
            { title: '主机名', dataIndex: 'hostname' },
            { title: '租用时间', dataIndex: 'leaseStart' },
            { title: '到期时间', dataIndex: 'leaseExpiry' },
          ]"
          row-key="address"
          size="small"
          :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'mac'">
              <span v-if="record.mac" class="font-mono">{{ record.mac }}</span>
              <span v-else>-</span>
            </template>
            <template v-else-if="column.dataIndex === 'hostname'">{{ record.hostname || '-' }}</template>
            <template v-else-if="column.dataIndex === 'leaseStart'">
              {{ record.leaseStart ? new Date(record.leaseStart).toLocaleString() : '-' }}
            </template>
            <template v-else-if="column.dataIndex === 'leaseExpiry'">
              {{ record.leaseExpiry ? new Date(record.leaseExpiry).toLocaleString() : '-' }}
            </template>
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
