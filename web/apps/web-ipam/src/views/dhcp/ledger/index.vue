<script setup lang="ts">
import type { OrgTreeNode, Subnet } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';

import { Card, Input, Menu, message, Table } from 'ant-design-vue';

import {
  bindStatic,
  bulkReservations,
  listLedger,
  listOrgTree,
  listSubnets,
  releaseAddress,
  reserveAddress,
} from '#/api/ipam';
import IpPlanMap from '#/components/ip-plan-map.vue';
import { $t } from '#/locales';
import { normalizeMacInput } from '#/utils/mac';

// ── IP 工具（与 IpPlanMap 同源）──
function ipToInt(ip: string): number {
  return (
    ip
      .split('.')
      .reduce((acc, o) => ((acc << 8) >>> 0) + Number.parseInt(o, 10), 0) >>> 0
  );
}
function intToIp(n: number): string {
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.');
}
function parseCidr(cidr: string): {
  broadcast: number;
  hostCount: number;
  network: number;
} {
  const parts = String(cidr).split('/');
  const bits = Number.parseInt(parts[1] || '32', 10);
  const ipInt = ipToInt(parts[0] || '0.0.0.0');
  const mask = bits === 0 ? 0 : (0xff_ff_ff_ff << (32 - bits)) >>> 0;
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
  const result = await listSubnets(selectedOrgId.value || undefined);
  subnets.value = result.items ?? [];
  if (v4Subnets.value.length > 0) {
    const cur = v4Subnets.value.find((s) => s.cidr === selectedCidr.value);
    const target = cur ?? v4Subnets.value[0];
    if (target) {
      selectedCidr.value = target.cidr;
      await loadMap();
    }
  } else {
    cells.value = [];
    selectedCidr.value = '';
  }
}

// ── 地址地图数据（IPv4 逐地址）──
interface MapCell {
  host: number;
  hostname?: string;
  ip: string;
  leaseEnd?: string;
  leaseStart?: string;
  leaseStatus?: string;
  mac?: string;
  overlays?: string[];
  purpose?: string;
  remark?: string;
  status: string;
  user?: string;
}
const cells = ref<MapCell[]>([]);
const mapLoading = ref(false);
const currentSubnet = computed(() =>
  v4Subnets.value.find((s) => s.cidr === selectedCidr.value),
);

/** ledger 状态 → 地图状态/叠加 */
function mapState(state: string): { overlays: string[]; status: string } {
  switch (state) {
    case 'conflict': {
      return { status: 'dynamic', overlays: ['conflict'] };
    }
    case 'grace': {
      return { status: 'dynamic', overlays: [] };
    }
    case 'online': {
      return { status: 'dynamic', overlays: ['online'] };
    }
    default: {
      return { status: state, overlays: [] };
    } // available/static/reserved 直通
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
        status:
          n === 0 ? 'network' : n === hostCount - 1 ? 'broadcast' : 'available',
        overlays: [],
      });
    }
    // 2) 覆盖台账状态（按 host 定位）
    const page = await listLedger({
      subnetId: sub.id,
      family: 4,
      pageSize: 500,
    });
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
        leaseStart: row.leaseStart
          ? new Date(row.leaseStart).toLocaleString()
          : '',
        leaseEnd: row.leaseExpiry
          ? new Date(row.leaseExpiry).toLocaleString()
          : '',
        leaseStatus:
          row.state === 'online' ? $t('dhcpLedger.common.leaseGranted') : '',
        purpose:
          row.state === 'reserved' ? $t('dhcpLedger.common.reserved') : '',
        remark:
          row.state === 'conflict'
            ? $t('dhcpLedger.common.ipConflict')
            : row.state === 'grace'
              ? $t('dhcpLedger.common.leaseGrace')
              : '',
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
const [BindModal, bindModalApi] = useVbenModal({
  draggable: true,
  confirmText: $t('dhcpLedger.common.bind'),
  onConfirm: () => confirmBind(),
});

async function onMapAction(action: string, ips: MapCell[]) {
  const sub = currentSubnet.value;
  const first = ips[0];
  if (!sub || !first) return;
  if (action === 'toStatic') {
    bindModal.value = { address: first.ip, subnetId: sub.id, mac: '' };
    bindModalApi.setState({
      title: $t('dhcpLedger.common.staticBindTitle', { ip: first.ip }),
    });
    bindModalApi.open();
    return;
  }
  try {
    if (action === 'toReserve') {
      const only = ips[0];
      if (ips.length === 1 && only) {
        await reserveAddress(sub.id, only.ip);
      } else {
        const res = await bulkReservations({
          subnetId: sub.id,
          entries: ips.map((c) => ({ address: c.ip, kind: 'reserve' })),
        });
        if (!res.ok) {
          const reasons = (res.failures ?? []).map((f) => f.reason).join('；');
          throw new Error(reasons || $t('dhcpLedger.common.bulkRolledBack'));
        }
      }
      message.success(
        $t('dhcpLedger.common.reservedCount', { count: ips.length }),
      );
    } else if (action === 'toRelease') {
      const failed: string[] = [];
      for (const c of ips) {
        try {
          await releaseAddress(c.ip);
        } catch {
          failed.push(c.ip);
        }
      }
      if (failed.length > 0) {
        message.warning(
          $t('dhcpLedger.common.releasedPartial', {
            ok: ips.length - failed.length,
            failed: failed.join('、'),
          }),
        );
      } else {
        message.success(
          $t('dhcpLedger.common.releasedCount', { count: ips.length }),
        );
      }
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.common.operationFailed'),
    );
  }
  void loadMap();
}
async function confirmBind() {
  const normMac = normalizeMacInput(bindModal.value.mac);
  if (!normMac || !bindModal.value.subnetId) {
    message.warning($t('dhcpLedger.common.invalidMac'));
    return;
  }
  try {
    await bindStatic(
      bindModal.value.subnetId,
      bindModal.value.address,
      normMac,
    );
    message.success(
      $t('dhcpLedger.common.staticBound', {
        address: bindModal.value.address,
        mac: normMac,
      }),
    );
    bindModalApi.close();
    void loadMap();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.common.bindFailed'),
    );
  }
}

// ── IPv6 网段表格 ──
const v6Cols = computed(() => [
  { title: $t('dhcpLedger.index.colCidr'), dataIndex: 'cidr' },
  { title: $t('dhcpLedger.index.colName'), dataIndex: 'name' },
  { title: $t('dhcpLedger.index.colPools'), dataIndex: 'pools' },
]);
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

onMounted(async () => {
  orgTree.value = await listOrgTree();
  await loadSubnets();
});
</script>

<template>
  <div class="p-4">
    <div class="flex gap-4">
      <Card
        :title="$t('dhcpLedger.index.orgCardTitle')"
        class="w-36 shrink-0 self-start"
        :body-style="{ padding: '2px 0' }"
      >
        <Menu
          class="org-menu max-h-[460px] overflow-auto"
          mode="vertical"
          :items="orgMenuItems"
          :selected-keys="selectedOrgId ? [selectedOrgId] : []"
          :inline-indent="10"
          @click="({ key }: any) => onSelectMenu(String(key))"
        />
        <div class="px-2 pb-1 pt-2 text-xs text-gray-400">
          {{ $t('dhcpLedger.index.orgHint') }}
        </div>
      </Card>

      <div class="min-w-0 flex-1">
        <div
          v-if="v4Subnets.length === 0 && v6Subnets.length === 0"
          class="rounded border border-dashed py-16 text-center text-gray-400"
        >
          {{ $t('dhcpLedger.index.pickOrgEmpty') }}
        </div>

        <!-- IPv4：地址地图（独立 Card，还原 ip-plan-map 原始布局） -->
        <IpPlanMap
          v-if="v4Subnets.length > 0"
          :cidr="selectedCidr"
          :ips="cells"
          :subnets="v4Subnets.map((s) => ({ cidr: s.cidr, name: s.name }))"
          @subnet-change="onSubnetChange"
          @action="onMapAction"
          @save="
            (_t: unknown, _v: unknown) =>
              message.info($t('dhcpLedger.index.readonlyInfo'))
          "
        />

        <!-- IPv6：网段表格 -->
        <Card
          v-if="v6Subnets.length > 0"
          :title="$t('dhcpLedger.index.v6CardTitle')"
          size="small"
          class="mt-4"
        >
          <template #extra>
            <span class="text-xs text-gray-400">{{
              $t('dhcpLedger.index.v6CardHint')
            }}</span>
          </template>
          <Table
            :data-source="v6Subnets"
            :columns="v6Cols"
            row-key="id"
            size="small"
            :pagination="false"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'pools'">
                {{ poolText(record as Subnet) }}
              </template>
            </template>
          </Table>
        </Card>
      </div>
    </div>

    <BindModal>
      <Input
        v-model:value="bindModal.mac"
        :placeholder="$t('dhcpLedger.common.macPlaceholder')"
        @press-enter="confirmBind"
      />
    </BindModal>
  </div>
</template>
<style scoped>
.org-menu :deep(.ant-menu-item),
.org-menu :deep(.ant-menu-submenu-title) {
  height: 32px;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  line-height: 32px;
  white-space: nowrap;
}

.org-menu :deep(.ant-menu) {
  border-inline-end: none;
}
</style>
