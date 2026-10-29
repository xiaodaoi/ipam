<script setup lang="ts">
// IpPlanMap.vue —— 由 tmp/ip-plan-map/IpPlanMap.jsx（React+antd5）移植的 IP 规划「地址地图」组件
// 功能：图形化地址地图（一格一 IP）/状态着色/叠加角标/选择/批量操作/编辑弹窗/详情抽屉/网段切换
import { computed, onMounted, reactive, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Alert,
  Button,
  Card,
  Descriptions,
  Divider,
  Drawer,
  Form,
  Input,
  message,
  Modal,
  Pagination,
  Select,
  Space,
  Tag,
  Tooltip,
} from 'ant-design-vue';

const props = withDefaults(
  defineProps<{
    cidr?: string;
    ips?: IpCell[];
    readOnly?: boolean;
    subnets?: { cidr: string; name?: string }[];
  }>(),
  { cidr: '10.59.193.0/24', ips: undefined, subnets: undefined, readOnly: false },
);
const emit = defineEmits<{
  (e: 'change', selected: string[], meta: { lastIp: null | string }): void;
  (e: 'action', action: string, ips: IpCell[]): void;
  (e: 'save', target: IpCell[], values: Record<string, any>): void;
  (e: 'subnetChange', cidr: string): void;
}>();

// ── IP 工具（纯函数）──
function ipToInt(ip: string): number {
  return ip.split('.').reduce((acc, o) => ((acc << 8) >>> 0) + Number.parseInt(o, 10), 0) >>> 0;
}
function intToIp(n: number): string {
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.');
}
function parseCidr(cidr: string): {
  bits: number; broadcast: number; hostCount: number; ip: string; ipInt: number; mask: number; network: number;
} {
  const parts = String(cidr).split('/');
  const bits = Number.parseInt(parts[1] || '32', 10);
  const ipInt = ipToInt(parts[0] || '0.0.0.0');
  const mask = bits === 0 ? 0 : (0xFF_FF_FF_FF << (32 - bits)) >>> 0;
  const network = (ipInt & mask) >>> 0;
  const broadcast = (network | (~mask >>> 0)) >>> 0;
  return { ip: parts[0] || '', bits, ipInt, mask, network, broadcast, hostCount: broadcast - network + 1 };
}

// ── 类型 ──
interface IpCell {
  ip: string;
  host: number;
  status: string;
  overlays?: string[];
  hostname?: string;
  mac?: string;
  leaseStatus?: string;
  leaseStart?: string;
  leaseEnd?: string;
  deviceName?: string;
  switchPort?: string;
  purpose?: string;
  user?: string;
  phone?: string;
  customAttrs?: Record<string, string>;
  remark?: string;
}

const IP_STATUS: Record<string, { color: string; label: string; }> = {
  network: { label: '网络', color: '#637196' },
  broadcast: { label: '广播', color: '#637196' },
  available: { label: '未规划', color: '#9DBEFF' },
  planned: { label: '已规划', color: '#BE86E4' },
  static: { label: '静态', color: '#2F54EB' },
  dynamic: { label: '动态', color: '#FFD666' },
  reserved: { label: '保留', color: '#FF9C6E' },
};
const OVERLAY_STATUS: Record<string, { color: string; label: string; }> = {
  zombie: { label: '僵尸', color: '#C41D7F' },
  conflict: { label: '冲突', color: '#FF6262' },
  online: { label: '在线', color: '#21BF86' },
};
const STATUS_DISABLED = new Set(['broadcast', 'network']);
const MAX_CELLS = 2048;
const PAGE_SIZE = 256;

function makeDefaultIps(cidr: string): IpCell[] {
  const { network, broadcast } = parseCidr(cidr);
  const list: IpCell[] = [];
  for (let n = network; n <= broadcast; n++) {
    const ip = intToIp(n);
    list.push({
      ip,
      host: n - network,
      status: n === network ? 'network' : (n === broadcast ? 'broadcast' : 'available'),
      overlays: [],
      hostname: '', leaseStatus: '', leaseStart: '', leaseEnd: '',
      deviceName: '', switchPort: '', purpose: '', user: '', phone: '',
      customAttrs: {}, remark: '',
    });
  }
  return list;
}

// ── 状态 ──
const currentCidr = computed(() => props.cidr || '10.59.193.0/24');
const internalIps = ref<IpCell[]>(makeDefaultIps(currentCidr.value));
const ipList = computed<IpCell[]>(() => props.ips ?? internalIps.value);

// ── 分页（每页一个 /24，256 格；选择/统计始终基于完整 ipList）──
const page = ref(1);
const pagedIps = computed<IpCell[]>(() =>
  ipList.value.slice((page.value - 1) * PAGE_SIZE, page.value * PAGE_SIZE),
);
// 当前页对应的 /24 基址（每页 256 格；用于分页条旁提示，避免跨页时对不上号）
const pageCidr = computed(() => {
  const first = pagedIps.value[0];
  if (!first) return '';
  return `${intToIp((ipToInt(first.ip) & 0xFF_FF_FF_00) >>> 0)}/24`;
});
watch(currentCidr, () => { page.value = 1; });

const selected = ref<string[]>([]);
const selectedSet = computed(() => new Set(selected.value));
const lastClickHost = ref<null | number>(null);

const hostToIp = computed(() => {
  const m = new Map<number, IpCell>();
  ipList.value.forEach((p) => m.set(p.host, p));
  return m;
});

const ipToCell = computed(() => {
  const m = new Map<string, IpCell>();
  ipList.value.forEach((p) => m.set(p.ip, p));
  return m;
});
const selectedIps = computed(() =>
  selected.value.map((ip) => ipToCell.value.get(ip)).filter((x): x is IpCell => !!x),
);

function updateSelected(next: string[], lastIp: null | string) {
  selected.value = next;
  emit('change', next, { lastIp });
}

function handleCellClick(ipObj: IpCell, evt: MouseEvent) {
  if (props.readOnly || STATUS_DISABLED.has(ipObj.status)) return;
  let next: string[];
  if (evt.shiftKey && lastClickHost.value !== null) {
    const lo = Math.min(ipObj.host, lastClickHost.value);
    const hi = Math.max(ipObj.host, lastClickHost.value);
    const range: string[] = [];
    for (let h = lo; h <= hi; h++) {
      const p = hostToIp.value.get(h);
      if (p && !STATUS_DISABLED.has(p.status)) range.push(p.ip);
    }
    next = [...new Set([...selected.value, ...range])];
  } else if (selectedSet.value.has(ipObj.ip)) {
    next = selected.value.filter((x) => x !== ipObj.ip);
  } else {
    next = [...selected.value, ipObj.ip];
  }
  lastClickHost.value = ipObj.host;
  updateSelected(next, ipObj.ip);
}

// ── 统计 ──
const stats = computed(() => {
  const planned = ipList.value.filter((p) => ['dynamic', 'planned', 'reserved', 'static'].includes(p.status)).length;
  const online = ipList.value.filter((p) => (p.overlays || []).includes('online') || p.status === 'online').length;
  const staticCount = ipList.value.filter((p) => p.status === 'static').length;
  const reservedCount = ipList.value.filter((p) => p.status === 'reserved').length;
  const conflictCount = ipList.value.filter((p) => (p.overlays || []).includes('conflict')).length;
  return { total: ipList.value.length, planned, online, static: staticCount, reserved: reservedCount, conflict: conflictCount, selected: selected.value.length };
});

const summaryItems = computed(() => [
  { color: IP_STATUS.dynamic?.color, key: 'online', label: '在线', value: stats.value.online },
  { color: IP_STATUS.static?.color, key: 'static', label: '静态', value: stats.value.static },
  { color: IP_STATUS.reserved?.color, key: 'reserved', label: '保留', value: stats.value.reserved },
  { color: IP_STATUS.planned?.color, key: 'planned', label: '已规划', value: stats.value.planned },
  { color: OVERLAY_STATUS.conflict?.color, key: 'conflict', label: '冲突', value: stats.value.conflict },
  { color: IP_STATUS.available?.color, key: 'selected', label: '已选中', value: stats.value.selected },
]);

// ── 网段切换 ──
function changeSubnet(next: number | string | undefined) {
  const cidr = String(next ?? '');
  emit('subnetChange', cidr);
  selected.value = [];
  lastClickHost.value = null;
  page.value = 1;
}
function prevSubnet() {
  const subs = props.subnets || [];
  const idx = subs.findIndex((s) => s.cidr === currentCidr.value);
  if (subs.length > 0 && idx > 0) { const s = subs[idx - 1]; if (s) changeSubnet(s.cidr); }
}
function nextSubnet() {
  const subs = props.subnets || [];
  const idx = subs.findIndex((s) => s.cidr === currentCidr.value);
  if (subs.length > 0 && idx !== -1 && idx < subs.length - 1) { const s = subs[idx + 1]; if (s) changeSubnet(s.cidr); }
}
const subnetOptions = computed(() =>
  (props.subnets || []).map((s) => ({ label: s.name ? `${s.name}（${s.cidr}）` : s.cidr, value: s.cidr })),
);

// ── 编辑弹窗 ──
const editOpen = ref(false);
const editing = ref<IpCell[]>([]);
const editForm = reactive({
  hostname: '', status: 'static', purpose: '', user: '', phone: '',
  customAttrText: '', remark: '',
});
function openEdit() {
  if (props.readOnly || selectedIps.value.length === 0) return;
  const first = selectedIps.value[0];
  if (!first) return;
  editing.value = selectedIps.value;
  Object.assign(editForm, {
    hostname: first.hostname || '',
    status: first.status,
    purpose: first.purpose || '',
    user: first.user || '',
    phone: first.phone || '',
    customAttrText: Object.entries(first.customAttrs || {}).map(([k, v]) => `${k}=${v}`).join('\n'),
    remark: first.remark || '',
  });
  editOpen.value = true;
}
function handleEditOk() {
  const customAttrs: Record<string, string> = {};
  String(editForm.customAttrText || '').split('\n').forEach((line) => {
    const i = line.indexOf('=');
    if (i > 0) customAttrs[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  });
  emit('save', editing.value, { ...editForm, customAttrs });
  editOpen.value = false;
}

// ── 详情抽屉 ──
const detail = ref<IpCell | null>(null);
function openDetail() {
  if (props.readOnly) return;
  if (selectedIps.value.length === 1) detail.value = selectedIps.value[0] ?? null;
  else message.info('请先只选中 1 个 IP 后再查看详情');
}

// ── 批量操作（消息与刷新由父层在 API 结果后处理，组件仅发事件）──
function applyStatus(status: string) {
  if (props.readOnly || selectedIps.value.length === 0) return;
  const ids = new Set(selectedIps.value.map((p) => p.ip));
  if (!props.ips) {
    internalIps.value = internalIps.value.map((p) => (ids.has(p.ip) ? { ...p, status } : p));
  }
  const actionName = { static: 'toStatic', reserved: 'toReserve', available: 'toRelease' }[status] || status;
  emit('action', actionName, selectedIps.value);
}

// ── 渲染辅助 ──
const parsed = computed(() => parseCidr(currentCidr.value));
onMounted(() => {
  if (!props.ips) internalIps.value = makeDefaultIps(currentCidr.value || '10.59.193.0/24');
});
</script>

<template>
  <Card size="small">
    <template #title>
      <span class="ipam-card-title">
        <IconifyIcon icon="lucide:layout-grid" />
        地址矩阵
      </span>
      <Tag color="blue" class="ml-2">{{ currentCidr }}</Tag>
      <Tag v-if="readOnly" class="ml-1">只读</Tag>
    </template>
    <template #extra>
      <Space :size="4" v-if="(subnets?.length || 0) > 0">
        <Button size="small" @click="prevSubnet" aria-label="上一个网段">
          <IconifyIcon icon="lucide:chevron-left" />
        </Button>
        <Select
          size="small"
          :value="currentCidr"
          :options="subnetOptions"
          @change="(v: any) => changeSubnet(String(v))"
          style="width: 220px"
          placeholder="选择网段"
        />
        <Button size="small" @click="nextSubnet" aria-label="下一个网段">
          <IconifyIcon icon="lucide:chevron-right" />
        </Button>
      </Space>
    </template>

    <!-- 边界保护 -->
    <Alert v-if="Number.isNaN(parsed.hostCount) || parsed.hostCount < 1" type="error" show-icon :message="`网段格式错误：${currentCidr}`" />
    <Alert
      v-else-if="parsed.hostCount > MAX_CELLS"
      type="warning"
      show-icon
      :message="`网段 ${currentCidr} 共 ${parsed.hostCount} 个地址，超出图形化展示上限（${MAX_CELLS}），建议规划更小的网段。`"
    />

    <template v-else>
      <!-- 统计 -->
      <div class="ipam-summary mb-3">
        <div v-for="item in summaryItems" :key="item.key" class="ipam-summary__item">
          <span class="ipam-summary__label">
            <i class="ipam-fam-dot" :style="{ backgroundColor: item.color }"></i>
            {{ item.label }}
          </span>
          <span class="ipam-summary__value">{{ item.value }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">地址总量</span>
          <span class="ipam-summary__value">{{ stats.total }}<small>个</small></span>
        </div>
      </div>

      <!-- 操作栏 -->
      <div class="ipam-toolbar mb-3">
        <Button size="small" :disabled="readOnly || !stats.selected" @click="applyStatus('static')">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1" />转静态
        </Button>
        <Button size="small" danger :disabled="readOnly || !stats.selected" @click="applyStatus('available')">
          <IconifyIcon icon="lucide:unlock" class="mr-1" />释放
        </Button>
        <Button size="small" :disabled="readOnly || !stats.selected" @click="applyStatus('reserved')">
          <IconifyIcon icon="lucide:lock" class="mr-1" />转保留
        </Button>
        <Button size="small" :disabled="readOnly || !stats.selected" @click="openEdit">
          <IconifyIcon icon="lucide:pencil" class="mr-1" />编辑
        </Button>
        <Button size="small" :disabled="readOnly || stats.selected !== 1" @click="openDetail">
          <IconifyIcon icon="lucide:info" class="mr-1" />详情
        </Button>
        <Button v-if="stats.selected > 0" size="small" danger @click="updateSelected([], null)">
          <IconifyIcon icon="lucide:eraser" class="mr-1" />清空选择
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">
          {{ stats.selected > 0 ? `已选 ${stats.selected} 个地址` : '点击格子选择，Shift+点击 连续区间' }}
        </span>
      </div>

      <!-- 图例 -->
      <div class="ipam-legend mb-3">
        <span v-for="k in Object.keys(IP_STATUS)" :key="k" class="ipam-legend__item">
          <i
            class="ipam-legend__swatch"
            :style="{ backgroundColor: IP_STATUS[k]?.color || '#ccc', opacity: STATUS_DISABLED.has(k) ? 0.75 : 1 }"
          ></i>
          {{ IP_STATUS[k]?.label || k }}
        </span>
        <span class="ipam-legend__divider"></span>
        <span class="ipam-legend__item">叠加状态</span>
        <span v-for="k in Object.keys(OVERLAY_STATUS)" :key="k" class="ipam-legend__item">
          <i
            class="ipam-legend__swatch"
            data-shape="dot"
            :style="{ backgroundColor: OVERLAY_STATUS[k]?.color || '#ccc' }"
          ></i>
          {{ OVERLAY_STATUS[k]?.label || k }}
        </span>
      </div>

      <Divider style="margin: 0 0 12px" />

      <!-- 地址地图 -->
      <div class="ip-grid">
        <Tooltip
          v-for="ipObj in pagedIps"
          :key="ipObj.ip"
          :mouse-enter-delay="0.2"
          placement="top"
        >
          <template #title>
            <div>
              <div style="font-weight: 600">{{ ipObj.ip }}</div>
              <div>状态：{{ IP_STATUS[ipObj.status]?.label || ipObj.status }}{{ (ipObj.overlays || []).length > 0 ? `（叠加：${(ipObj.overlays || []).map((o) => OVERLAY_STATUS[o]?.label || o).join('、')}）` : '' }}</div>
              <div v-if="ipObj.hostname">主机名：{{ ipObj.hostname }}</div>
              <div v-if="ipObj.mac">MAC：{{ ipObj.mac }}</div>
              <div v-if="ipObj.leaseStart">租用时间：{{ ipObj.leaseStart }}</div>
              <div v-if="ipObj.leaseEnd">到期时间：{{ ipObj.leaseEnd }}</div>
              <div v-if="ipObj.user">使用人：{{ ipObj.user }}</div>
            </div>
          </template>
          <div
            class="ip-cell"
            @click="handleCellClick(ipObj, $event)"
            :style="{
              backgroundColor: IP_STATUS[ipObj.status]?.color ?? '#9DBEFF',
              border: selectedSet.has(ipObj.ip) ? '2px dashed #000' : '1px solid #e6e9f4',
              opacity: readOnly || STATUS_DISABLED.has(ipObj.status) ? 0.8 : 1,
            }"
          >
            {{ ipObj.host % PAGE_SIZE }}
            <span
              v-for="(ov, i) in (ipObj.overlays || []).slice(0, 2)"
              :key="ov"
              :style="{
                position: 'absolute', right: `${2 + i * 9}px`, bottom: '2px',
                width: '7px', height: '7px', borderRadius: '50%',
                backgroundColor: OVERLAY_STATUS[ov]?.color || '#999', border: '1px solid #fff',
              }"
            ></span>
          </div>
        </Tooltip>
      </div>

      <!-- 分页（超过一个 /24 时显示，右下角） -->
      <div v-if="ipList.length > PAGE_SIZE" style="display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 12px">
        <span class="ipam-hint">{{ pageCidr }}</span>
        <Pagination
          v-if="ipList.length > PAGE_SIZE"
          :current="page"
          :page-size="PAGE_SIZE"
          :total="ipList.length"
          :show-size-changer="false"
          show-quick-jumper
          size="small"
          @change="(p: number) => { page = p }"
        />
      </div>

      <!-- 底部提示 -->
      <div class="ipam-hint mt-2">
        点击选择 IP · Shift+点击 区间选择 · 网络/广播地址不可操作{{ readOnly ? ' · 当前为只读模式' : '' }}
      </div>

      <!-- 编辑弹窗 -->
      <Modal
        :open="editOpen"
        :title="editing.length > 1 ? `编辑 ${editing.length} 个 IP` : `编辑 IP · ${editing[0]?.ip || ''}`"
        ok-text="保存"
        :width="520"
        @ok="handleEditOk"
        @cancel="editOpen = false"
      >
        <Form layout="vertical">
          <Form.Item label="状态">
            <Select
              v-model:value="editForm.status"
              :options="Object.entries(IP_STATUS).filter(([k]) => !STATUS_DISABLED.has(k)).map(([k, v]) => ({ label: v.label, value: k }))"
            />
          </Form.Item>
          <div class="grid grid-cols-2 gap-x-3 gap-y-0">
            <Form.Item label="主机名">
              <Input v-model:value="editForm.hostname" placeholder="如 PC-001" />
            </Form.Item>
            <Form.Item label="用途">
              <Input v-model:value="editForm.purpose" placeholder="办公 / 服务器 / 打印机…" />
            </Form.Item>
            <Form.Item label="使用人">
              <Input v-model:value="editForm.user" />
            </Form.Item>
            <Form.Item label="电话">
              <Input v-model:value="editForm.phone" />
            </Form.Item>
          </div>
          <Form.Item label="自定义属性" extra="每行一条，格式：键=值">
            <Input.TextArea v-model:value="editForm.customAttrText" :rows="2" placeholder="资产编号=ZC-001&#10;位置=三楼机房" />
          </Form.Item>
          <Form.Item label="备注" class="mb-0">
            <Input.TextArea v-model:value="editForm.remark" :rows="2" />
          </Form.Item>
        </Form>
      </Modal>

      <!-- 详情抽屉 -->
      <Drawer :open="!!detail" :title="`IP 详情 · ${detail?.ip || ''}`" :width="420" @close="detail = null">
        <template v-if="detail">
          <Space wrap style="margin-bottom: 12px">
            <Tag :color="IP_STATUS[detail.status]?.color">{{ IP_STATUS[detail.status]?.label || detail.status }}</Tag>
            <Tag v-for="o in (detail.overlays || [])" :key="o" :color="OVERLAY_STATUS[o]?.color">
              {{ OVERLAY_STATUS[o]?.label || o }}
            </Tag>
          </Space>

          <Divider orientation="left" plain style="margin: 8px 0">分配信息</Divider>
          <Descriptions :column="1" size="small" bordered>
            <Descriptions.Item label="地址">{{ detail.ip }}</Descriptions.Item>
            <Descriptions.Item label="主机名">{{ detail.hostname || '-' }}</Descriptions.Item>
            <Descriptions.Item label="租约状态">{{ detail.leaseStatus || '-' }}</Descriptions.Item>
            <Descriptions.Item label="租约时间">{{ detail.leaseStart || '-' }}</Descriptions.Item>
            <Descriptions.Item label="到期时间">{{ detail.leaseEnd || '-' }}</Descriptions.Item>
          </Descriptions>

          <Divider orientation="left" plain style="margin: 12px 0">扫描信息</Divider>
          <Descriptions :column="1" size="small" bordered>
            <Descriptions.Item label="设备名称">{{ detail.deviceName || '-' }}</Descriptions.Item>
            <Descriptions.Item label="交换机端口">{{ detail.switchPort || '-' }}</Descriptions.Item>
          </Descriptions>

          <Divider orientation="left" plain style="margin: 12px 0">资产信息</Divider>
          <Descriptions :column="1" size="small" bordered>
            <Descriptions.Item v-if="Object.keys(detail.customAttrs || {}).length === 0" label="资产信息">-</Descriptions.Item>
            <Descriptions.Item v-for="(v, k) in detail.customAttrs || {}" :key="k" :label="String(k)">{{ String(v) }}</Descriptions.Item>
          </Descriptions>

          <Divider orientation="left" plain style="margin: 12px 0">管理信息</Divider>
          <Descriptions :column="1" size="small" bordered>
            <Descriptions.Item label="用途">{{ detail.purpose || '-' }}</Descriptions.Item>
            <Descriptions.Item label="使用人">{{ detail.user || '-' }}</Descriptions.Item>
            <Descriptions.Item label="电话">{{ detail.phone || '-' }}</Descriptions.Item>
            <Descriptions.Item label="备注">{{ detail.remark || '-' }}</Descriptions.Item>
          </Descriptions>
        </template>
      </Drawer>
    </template>
  </Card>
</template>
<style scoped>
.ip-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  max-height: 420px;
  overflow: auto;
  padding-bottom: 4px;
}
.ip-cell {
  width: 32px;
  height: 32px;
  font-size: 11px;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  user-select: none;
  position: relative;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  box-sizing: border-box;
  border: 1px solid #e6e9f4;
  cursor: pointer;
}
</style>
