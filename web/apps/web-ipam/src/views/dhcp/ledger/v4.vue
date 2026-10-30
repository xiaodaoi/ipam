<script setup lang="ts">
import type { LedgerRow, OrgTreeNode, Subnet } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Select, Space, Table, Tag } from 'ant-design-vue';

import { bindStatic, listLedger, listOrgTree, listSubnets, releaseAddress, reserveAddress, upsertLedgerMeta } from '#/api/ipam';
import IpPlanMap from '#/components/ip-plan-map.vue';
import OrgFilterCard from '#/components/org-filter-card.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { normalizeMacInput } from '#/utils/mac';

const showTotal = (t: number) => `共 ${t} 条`;

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
  const res = await listSubnets(selectedOrgId.value || undefined);
  subnets.value = res.items ?? [];
  const first = v4Subnets.value[0];
  if (first) {
    const cur = v4Subnets.value.find((s) => s.cidr === selectedCidr.value);
    selectedCidr.value = (cur ?? first).cidr;
    await loadMap();
  } else {
    cells.value = [];
    onlineRows.value = [];
    selectedCidr.value = '';
  }
}

// ── 地址矩阵数据（IPv4 逐地址）──
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
  phone?: string;
  vlan?: number;
  site?: string;
  customAttrs?: Record<string, string>;
  remark?: string;
  leaseStatus?: string;
}
const cells = ref<MapCell[]>([]);
const onlineRows = ref<Partial<LedgerRow>[]>([]);
const mapLoading = ref(false);
const currentSubnet = computed(() => v4Subnets.value.find((s) => s.cidr === selectedCidr.value));

/** 当前网段地址矩阵统计（网络/广播地址不计入可用容量） */
const stats = computed(() => {
  let available = 0;
  let conflict = 0;
  let online = 0;
  let reserved = 0;
  let staticCount = 0;
  let usable = 0;
  for (const c of cells.value) {
    if (c.status === 'network' || c.status === 'broadcast') continue;
    usable += 1;
    if (c.status === 'available') available += 1;
    if (c.status === 'reserved') reserved += 1;
    if (c.status === 'static') staticCount += 1;
    if ((c.overlays ?? []).includes('online')) online += 1;
    if ((c.overlays ?? []).includes('conflict')) conflict += 1;
  }
  const used = Math.max(0, usable - available);
  const utilization = usable > 0 ? Math.round((used / usable) * 1000) / 10 : 0;
  return {
    available,
    conflict,
    online,
    reserved,
    staticCount,
    total: cells.value.length,
    usable,
    used,
    utilization,
  };
});

/** 地址池摘要（当前子网） */
const poolText = computed(() => {
  const ps = currentSubnet.value?.pools ?? [];
  const first = ps[0];
  if (!first) return '未配置地址池';
  const one = `${first.startAddr} — ${first.endAddr ?? ''}`;
  return ps.length > 1 ? `${one} +${ps.length - 1}` : one;
});

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

/** 在线租约的“现在”基准，随每次刷新更新，避免长时间停留后到期提示失真 */
const nowTs = ref(Date.now());

async function loadMap() {
  const sub = currentSubnet.value;
  if (!sub) {
    cells.value = [];
    onlineRows.value = [];
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
        status: n === 0 ? 'network' : (n === hostCount - 1 ? 'broadcast' : 'available'),
        overlays: [],
      });
    }
    // 2) 覆盖台账状态（按 host 定位）
    const page = await listLedger({ subnetId: sub.id, family: 4, pageSize: 500 });
    onlineRows.value = (page.items ?? []).filter((r) => r.state === 'online');
    nowTs.value = Date.now();
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
        purpose: row.purpose || (row.state === 'reserved' ? '保留' : ''),
        phone: row.phone || '',
        vlan: row.vlan,
        site: row.site || '',
        customAttrs: row.customAttrs ?? {},
        remark: row.remark || (row.state === 'conflict' ? 'IP 冲突' : (row.state === 'grace' ? '租约宽限' : '')),
      };
    }
    cells.value = grid;
  } finally {
    mapLoading.value = false;
  }
}

// ── 在线租约：关键字 / 到期窗口筛选 ──
const onlineKeyword = ref('');
type ExpiryFilter = '7d' | '24h' | 'all' | 'expired';
const expiryFilter = ref<ExpiryFilter>('all');
const expiryOptions = [
  { label: '全部到期状态', value: 'all' },
  { label: '已过期', value: 'expired' },
  { label: '24 小时内到期', value: '24h' },
  { label: '7 天内到期', value: '7d' },
];

function remainingMs(row: Partial<LedgerRow>): null | number {
  if (!row.leaseExpiry) return null;
  const t = new Date(row.leaseExpiry).getTime();
  return Number.isNaN(t) ? null : t - nowTs.value;
}

function expiryTone(row: Partial<LedgerRow>): string {
  const ms = remainingMs(row);
  if (ms === null) return 'muted';
  if (ms < 0) return 'red';
  if (ms < 86_400_000) return 'amber';
  if (ms < 7 * 86_400_000) return 'cyan';
  return 'green';
}

function fmtDuration(ms: number): string {
  const mins = Math.max(1, Math.round(Math.abs(ms) / 60_000));
  if (mins < 60) return `${mins} 分钟`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `${hours} 小时`;
  return `${Math.round(hours / 24)} 天`;
}

function expiryText(row: Partial<LedgerRow>): string {
  const ms = remainingMs(row);
  if (ms === null) return '无租期';
  return ms < 0 ? `已过期 ${fmtDuration(ms)}` : `剩余 ${fmtDuration(ms)}`;
}

function fmtTime(v?: string): string {
  return v ? new Date(v).toLocaleString() : '—';
}

const filteredOnlineRows = computed(() => {
  const kw = onlineKeyword.value.trim().toLowerCase();
  return onlineRows.value.filter((row) => {
    const ms = remainingMs(row);
    if (expiryFilter.value === 'expired' && (!(ms !== null && ms < 0))) return false;
    if (expiryFilter.value === '24h' && (!(ms !== null && ms >= 0 && ms <= 86_400_000))) return false;
    if (expiryFilter.value === '7d' && (!(ms !== null && ms >= 0 && ms <= 7 * 86_400_000))) return false;
    if (!kw) return true;
    return [row.address, row.mac, row.hostname, row.owner].some((v) =>
      String(v ?? '').toLowerCase().includes(kw),
    );
  });
});

function resetOnlineFilters() {
  onlineKeyword.value = '';
  expiryFilter.value = 'all';
}

// ── IP 工具（与 IpPlanMap 同源）──
function ipToInt(ip: string): number {
  return ip.split('.').reduce((acc, o) => ((acc << 8) >>> 0) + Number.parseInt(o, 10), 0) >>> 0;
}
function intToIp(n: number): string {
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.');
}
function parseCidr(cidr: string): { broadcast: number; hostCount: number; network: number } {
  const parts = String(cidr).split('/');
  const bits = Number.parseInt(parts[1] || '32', 10);
  const ipInt = ipToInt(parts[0] || '0.0.0.0');
  const mask = bits === 0 ? 0 : (0xFF_FF_FF_FF << (32 - bits)) >>> 0;
  const network = (ipInt & mask) >>> 0;
  const broadcast = (network | (~mask >>> 0)) >>> 0;
  return { network, broadcast, hostCount: broadcast - network + 1 };
}

function onSubnetChange(cidr: string) {
  selectedCidr.value = cidr;
  onlineKeyword.value = '';
  expiryFilter.value = 'all';
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
        await reserveAddress(sub.id, first.ip);
      } else {
        const { bulkReservations } = await import('#/api/ipam');
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
      if (failed.length > 0) {
        message.warning(`已释放 ${ips.length - failed.length} 个，失败（无预留记录）：${failed.join('、')}`);
      } else {
        message.success(`已释放 ${ips.length} 个地址，回归可下发池`);
      }
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
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
  } catch (error) {
    message.error(error instanceof Error ? error.message : '绑定失败');
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
// ── 逐 IP 元数据保存（VLAN/站点/使用人/用途/备注）──
async function saveLedgerMeta(target: MapCell[], values: Record<string, any>) {
  const sub = currentSubnet.value;
  const list = target.filter((c) => c.status !== 'network' && c.status !== 'broadcast');
  if (!sub || list.length === 0) return;
  try {
    await Promise.all(
      list.map((c) =>
        upsertLedgerMeta({
          address: c.ip,
          subnetId: sub.id,
          purpose: values.purpose || undefined,
          user: values.user || undefined,
          phone: values.phone || undefined,
          site: values.site || undefined,
          vlan: values.vlan ?? undefined,
          remark: values.remark || undefined,
          customAttrs: (values.customAttrs ?? {}) as Record<string, string>,
        }),
      ),
    );
    message.success(`已保存 ${list.length} 个地址的台账信息`);
    await loadMap();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '台账信息保存失败');
  }
}

// ── CSV 导出（当前网段地址矩阵）──
function csvCell(v: unknown): string {
  const s = String(v ?? '');
  return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
}

function exportLedgerCsv() {
  if (!selectedCidr.value || cells.value.length === 0) {
    message.warning('当前网段暂无可导出的地址');
    return;
  }
  const header = ['IP', '状态', '主机名', 'MAC', '使用人', '用途', '备注', '租约开始', '租约结束'];
  const lines = cells.value.map((c) =>
    [c.ip, c.status, c.hostname, c.mac, c.user, c.purpose, c.remark, c.leaseStart, c.leaseEnd]
      .map((v) => csvCell(v))
      .join(','),
  );
  const csv = `\uFEFF${[header.join(','), ...lines].join('\r\n')}`;
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `ledger-${selectedCidr.value.replaceAll('/', '_')}-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  URL.revokeObjectURL(a.href);
  message.success(`已导出 ${cells.value.length} 个地址`);
}
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Service"
      icon="lucide:map"
      title="IPv4 地址台账"
      description="按地址矩阵查看 IPv4 分配状态，支持单点/区间选择、保留、静态绑定与释放；下方在线租约支持检索与到期提醒。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layers" class="size-3.5" />
          {{ v4Subnets.length }} 个 IPv4 网段 · 当前 {{ selectedCidr || '未选择' }}
        </span>
        <Button size="small" :disabled="!selectedCidr || cells.length === 0" @click="exportLedgerCsv">
          <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />
          导出 CSV
        </Button>
        <Button size="small" :loading="mapLoading" :disabled="!selectedCidr" @click="loadMap">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />
          刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-subnets-layout">
      <OrgFilterCard
        :org-tree="orgTree"
        :selected-org-id="selectedOrgId"
        @select="onSelectOrg"
      />

      <div class="ipam-subnets-main">
        <div v-if="v4Subnets.length === 0" class="ipam-empty">
          <div class="ipam-empty__icon">
            <IconifyIcon icon="lucide:map" class="size-5" />
          </div>
          <div class="ipam-empty__title">暂无 IPv4 网段</div>
          <div class="ipam-empty__desc">请先在左侧选择组织，或前往「子网管理」创建 IPv4 网段。</div>
        </div>

        <template v-else>
          <div class="ipam-stat-grid">
            <StatCard
              label="地址容量"
              :value="stats.usable"
              unit="个"
              icon="lucide:hash"
              tone="blue"
              :hint="`含网络/广播共 ${stats.total} 个地址`"
            />
            <StatCard
              label="已用地址"
              :value="stats.used"
              unit="个"
              icon="lucide:pie-chart"
              tone="green"
              :progress="stats.utilization"
              :hint="`利用率 ${stats.utilization}% · 在线 ${stats.online} 个`"
            />
            <StatCard
              label="静态绑定"
              :value="stats.staticCount"
              unit="个"
              icon="lucide:pin"
              tone="cyan"
              :hint="`保留 ${stats.reserved} 个地址`"
            />
            <StatCard
              label="可分配余量"
              :value="stats.available"
              unit="个"
              icon="lucide:circle-dashed"
              :tone="stats.conflict > 0 ? 'amber' : 'violet'"
              :hint="stats.conflict > 0 ? `${stats.conflict} 个地址存在冲突` : '当前网段无冲突'"
            />
          </div>

          <div class="ipam-toolbar">
            <span class="ipam-field">
              <span class="ipam-field__label">当前网段</span>
              <Tag color="blue" class="m-0">{{ selectedCidr }}</Tag>
              <span class="ipam-hint">{{ currentSubnet?.name || '未命名网段' }}</span>
            </span>
            <span class="ipam-field">
              <span class="ipam-field__label">网关</span>
              <span class="ipam-mono">{{ currentSubnet?.gateway || '未配置' }}</span>
            </span>
            <span class="ipam-field">
              <span class="ipam-field__label">DNS</span>
              <span class="ipam-mono">{{ currentSubnet?.dnsServers || '未配置' }}</span>
            </span>
            <span class="ipam-field">
              <span class="ipam-field__label">地址池</span>
              <span class="ipam-mono">{{ poolText }}</span>
            </span>
            <div class="ipam-toolbar__spacer"></div>
            <span class="ipam-hint">在地址矩阵中选中地址后可批量转静态 / 保留 / 释放</span>
          </div>

          <IpPlanMap
            :cidr="selectedCidr"
            :ips="cells"
            :subnets="v4Subnets.map((s) => ({ cidr: s.cidr, name: s.name }))"
            @subnet-change="onSubnetChange"
            @action="onMapAction"
            @save="saveLedgerMeta"
          />

          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:radio-tower" />
                在线租约
                <em class="ipam-card-sub">
                  {{ selectedCidr || '未选择网段' }} · {{ filteredOnlineRows.length }} /
                  {{ onlineRows.length }} 条
                </em>
              </span>
            </template>
            <template #extra>
              <Space :size="6" wrap>
                <Tag v-if="stats.conflict > 0" color="red" class="m-0">冲突 {{ stats.conflict }}</Tag>
                <span class="ipam-hint">随 DHCP 租约实时更新</span>
              </Space>
            </template>

            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">关键字</span>
                <Input
                  v-model:value="onlineKeyword"
                  allow-clear
                  placeholder="IP / MAC / 主机名 / 使用人"
                  style="width: 240px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">到期</span>
                <Select
                  v-model:value="expiryFilter"
                  :options="expiryOptions"
                  style="width: 150px"
                />
              </div>
              <Button size="small" @click="resetOnlineFilters">重置</Button>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">到期时间按浏览器本地时区显示</span>
            </div>

            <Table
              :data-source="filteredOnlineRows"
              :loading="mapLoading"
              :columns="[
                { title: '在线地址', dataIndex: 'address', width: 130, sorter: (a: any, b: any) => ipToInt(a.address) - ipToInt(b.address) },
                { title: 'MAC', dataIndex: 'mac', width: 160 },
                { title: '主机名', dataIndex: 'hostname', width: 140 },
                { title: '使用人', dataIndex: 'owner', width: 100 },
                { title: '到期时间', dataIndex: 'leaseExpiry', width: 220 },
              ]"
              :locale="{ emptyText: onlineKeyword || expiryFilter !== 'all' ? '没有匹配的在线租约' : '当前网段暂无在线租约' }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              row-key="address"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'address'">
                  <span class="ipam-mono">{{ record.address }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'mac'">
                  <span v-if="record.mac" class="ipam-mono">{{ record.mac }}</span>
                  <span v-else class="text-gray-400">—</span>
                </template>
                <template v-else-if="column.dataIndex === 'hostname'">{{ record.hostname || '—' }}</template>
                <template v-else-if="column.dataIndex === 'owner'">{{ record.owner || '—' }}</template>
                <template v-else-if="column.dataIndex === 'leaseExpiry'">
                  <div class="ipam-cell-stack">
                    <span class="ipam-hint">{{ fmtTime(record.leaseExpiry) }}</span>
                    <span class="ipam-chip" :data-tone="expiryTone(record)">{{ expiryText(record) }}</span>
                  </div>
                </template>
              </template>
            </Table>
          </Card>
        </template>
      </div>
    </div>

    <BindModal>
      <Input v-model:value="bindModal.mac" placeholder="MAC 如 aa:bb:cc:dd:ee:01" @press-enter="confirmBind" />
    </BindModal>
  </div>
</template>
