<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Select, Switch, Tag } from 'ant-design-vue';

import { openLogTail } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

interface TailRow {
  action?: string;
  answerIp?: string;
  category?: string;
  clientIp?: string;
  clientMac?: string;
  detail?: string;
  domain?: string;
  rcode?: string;
  severity?: string;
  sip?: string;
  ts: string;
  type: string;
}

const BUFFER_OPTIONS = [200, 500, 1000, 2000].map((n) => ({
  label: `最近 ${n} 条`,
  value: n,
}));
const STATUS_META = {
  connecting: { color: 'processing', text: '连接中' },
  live: { color: 'success', text: '实时接收' },
  reconnecting: { color: 'warning', text: '重连中…' },
} as const;

const rows = ref<TailRow[]>([]);
const pending = ref<TailRow[]>([]);
const status = ref<'connecting' | 'live' | 'reconnecting'>('connecting');
const paused = ref(false);
const autoScroll = ref(true);
const onlyBlocked = ref(false);
const keyword = ref('');
const filterType = ref<string>();
const bufferSize = ref(500);
const received = ref(0);
const rate = ref(0);
const lastArrival = ref('');
const bodyRef = ref<HTMLElement>();

let es: undefined | { close(): void };
let retryTimer: ReturnType<typeof setTimeout> | undefined;
let rateTimer: ReturnType<typeof setInterval> | undefined;
let retryDelay = 2000;
let disposed = false;
let arrivals: number[] = [];

function parseTs(v?: string): Date | undefined {
  if (!v) return undefined;
  const t = Date.parse(v);
  return Number.isNaN(t) || t <= 0 ? undefined : new Date(t);
}
function fmtTime(v?: string): string {
  const d = parseTs(v);
  if (!d) return '—';
  return d.toLocaleTimeString('zh-CN', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    fractionalSecondDigits: 3,
  });
}
function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString('zh-CN');
}
function fmtMac(mac?: string): string {
  if (!mac) return '';
  const hex = mac.replaceAll(/[^0-9a-f]/gi, '').toLowerCase();
  return hex.length === 12 ? (hex.match(/.{2}/g) ?? []).join(':') : mac;
}

function trimBuffer() {
  if (rows.value.length > bufferSize.value) {
    rows.value = rows.value.slice(0, bufferSize.value);
  }
  if (pending.value.length > bufferSize.value) {
    pending.value = pending.value.slice(0, bufferSize.value);
  }
}

function scrollToTop() {
  if (!autoScroll.value) return;
  void nextTick(() => {
    if (bodyRef.value) bodyRef.value.scrollTop = 0;
  });
}

function onRow(row: TailRow) {
  received.value += 1;
  lastArrival.value = row.ts;
  arrivals.push(Date.now());
  if (arrivals.length > 5000) arrivals = arrivals.slice(-2000);
  if (paused.value) {
    pending.value = [row, ...pending.value].slice(0, bufferSize.value);
    return;
  }
  rows.value = [row, ...rows.value].slice(0, bufferSize.value);
  scrollToTop();
}

function connect() {
  if (disposed) return;
  clearTimeout(retryTimer);
  retryTimer = undefined;
  es?.close();
  status.value = 'connecting';
  es = openLogTail(
    { type: filterType.value },
    {
      onRow: (row: TailRow) => onRow(row),
      onOpen: () => {
        status.value = 'live';
        retryDelay = 2000;
      },
      onError: () => {
        if (disposed) return;
        status.value = 'reconnecting';
        scheduleReconnect();
      },
    },
  );
}

function scheduleReconnect() {
  clearTimeout(retryTimer);
  retryTimer = setTimeout(() => {
    connect();
  }, retryDelay);
  retryDelay = Math.min(15_000, retryDelay * 2);
}

function reload() {
  rows.value = [];
  pending.value = [];
  received.value = 0;
  rate.value = 0;
  paused.value = false;
  retryDelay = 2000;
  arrivals = [];
  connect();
}

function togglePause() {
  paused.value = !paused.value;
  if (!paused.value && pending.value.length > 0) {
    rows.value = [...pending.value, ...rows.value].slice(0, bufferSize.value);
    pending.value = [];
    scrollToTop();
  }
}

function clearRows() {
  rows.value = [];
  pending.value = [];
}

watch(bufferSize, trimBuffer);
watch(autoScroll, (on) => {
  if (on) scrollToTop();
});

const blockedCount = computed(
  () => rows.value.filter((r) => r.action === 'blocked').length,
);

function hit(r: TailRow): boolean {
  if (onlyBlocked.value && r.action !== 'blocked') return false;
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return true;
  return [
    r.domain,
    r.clientMac,
    r.clientIp,
    r.sip,
    r.answerIp,
    r.rcode,
    r.action,
    r.detail,
  ].some((v) => v && String(v).toLowerCase().includes(kw));
}

const filtered = computed(() => rows.value.filter((r) => hit(r)));

interface Segment {
  hit: boolean;
  text: string;
}

/** 关键字高亮：返回分段文本，模板用插值渲染，避免 v-html 注入风险 */
function segments(src = ''): Segment[] {
  const kw = keyword.value.trim();
  if (!kw) return [{ hit: false, text: src }];
  const lower = src.toLowerCase();
  const needle = kw.toLowerCase();
  const out: Segment[] = [];
  let i = 0;
  while (i < src.length) {
    const j = lower.indexOf(needle, i);
    if (j === -1) {
      out.push({ hit: false, text: src.slice(i) });
      break;
    }
    if (j > i) out.push({ hit: false, text: src.slice(i, j) });
    out.push({ hit: true, text: src.slice(j, j + needle.length) });
    i = j + needle.length;
  }
  return out;
}

function shortDetail(v?: string): string {
  if (!v) return '';
  return v.length > 160 ? `${v.slice(0, 160)}…` : v;
}

function exportTxt() {
  if (filtered.value.length === 0) {
    message.warning('当前没有可导出的日志行');
    return;
  }
  const lines = filtered.value.map((r) =>
    [
      fmtTime(r.ts),
      r.type.toUpperCase(),
      r.domain,
      r.clientIp,
      fmtMac(r.clientMac),
      r.sip,
      r.answerIp,
      r.rcode,
      r.action,
      r.detail,
    ]
      .filter(Boolean)
      .join('\t'),
  );
  const blob = new Blob([lines.join('\n')], {
    type: 'text/plain;charset=utf-8',
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  const stamp = new Date()
    .toISOString()
    .slice(0, 19)
    .replaceAll(/[:T]/g, '');
  a.href = url;
  a.download = `ipam-tail-${stamp}.txt`;
  a.click();
  URL.revokeObjectURL(url);
  message.success(`已导出 ${filtered.value.length} 行`);
}

async function copyVisible() {
  if (filtered.value.length === 0) {
    message.warning('当前没有可复制的日志行');
    return;
  }
  const text = filtered.value
    .map((r) => `${fmtTime(r.ts)} ${r.type} ${r.domain ?? ''} ${r.action ?? ''} ${r.detail ?? ''}`)
    .join('\n');
  try {
    await navigator.clipboard.writeText(text);
    message.success(`已复制 ${filtered.value.length} 行`);
  } catch {
    message.warning('浏览器未授予剪贴板权限');
  }
}

onMounted(() => {
  connect();
  rateTimer = setInterval(() => {
    const cut = Date.now() - 5000;
    arrivals = arrivals.filter((t) => t >= cut);
    rate.value = arrivals.length / 5;
  }, 1000);
});

onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(retryTimer);
  clearInterval(rateTimer);
  es?.close();
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:radio"
      title="实时日志流"
      description="SSE 实时接收 DHCP 与 DNS 事件，支持暂停缓存、关键字高亮、自动滚动与本地导出，用于现场排障与演示。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <span
            class="ipam-status-dot"
            :data-state="status === 'live' ? 'up' : status === 'reconnecting' ? 'down' : 'unknown'"
          ></span>
          {{ STATUS_META[status].text }} · 已接收 {{ fmtInt(received) }} 条
        </span>
        <Button size="small" @click="reload">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />重新连接
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="连接状态"
        :value="STATUS_META[status].text"
        icon="lucide:plug-zap"
        :tone="status === 'live' ? 'green' : status === 'reconnecting' ? 'amber' : 'blue'"
        :hint="status === 'live' ? 'SSE 通道已建立，事件实时推送' : '断线后自动退避重连，最长 15 秒'"
      />
      <StatCard
        label="已接收事件"
        :value="fmtInt(received)"
        icon="lucide:inbox"
        tone="blue"
        :hint="lastArrival ? `最近事件 ${fmtTime(lastArrival)}` : '等待首条事件…'"
      />
      <StatCard
        label="当前显示"
        :value="fmtInt(filtered.length)"
        icon="lucide:list-filter"
        tone="cyan"
        :hint="keyword || onlyBlocked ? '已按关键字/拦截条件过滤' : `缓冲区 ${rows.length}/${bufferSize} 条`"
      />
      <StatCard
        label="接收速率"
        :value="rate.toFixed(1)"
        unit="条/秒"
        icon="lucide:gauge"
        :tone="rate > 0 ? 'green' : 'violet'"
        :hint="`近 5 秒滑动平均 · 拦截 ${blockedCount} 条`"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:terminal" />
          事件流
          <em class="ipam-card-sub">events.tail</em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <Tag class="m-0" :color="STATUS_META[status].color">
            {{ STATUS_META[status].text }}
          </Tag>
          <span v-if="paused" class="ipam-chip" data-tone="amber">
            已暂停 · 缓存 {{ pending.length }} 条
          </span>
          <Button size="small" :type="paused ? 'primary' : 'default'" @click="togglePause">
            <IconifyIcon :icon="paused ? 'lucide:play' : 'lucide:pause'" class="mr-1 size-3.5" />
            {{ paused ? '恢复' : '暂停' }}
          </Button>
          <Button size="small" @click="clearRows">
            <IconifyIcon icon="lucide:eraser" class="mr-1 size-3.5" />清空
          </Button>
        </div>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">类型</span>
          <Select
            v-model:value="filterType"
            style="width: 120px"
            allow-clear
            placeholder="全部"
            :options="[
              { value: 'dhcp', label: 'DHCP' },
              { value: 'dns', label: 'DNS' },
            ]"
            @change="reload"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input
            v-model:value="keyword"
            allow-clear
            placeholder="域名 / MAC / IP / 详情"
            style="width: 200px"
          />
        </div>
        <Button
          size="small"
          :type="onlyBlocked ? 'primary' : 'default'"
          @click="onlyBlocked = !onlyBlocked"
        >
          <IconifyIcon icon="lucide:shield-ban" class="mr-1 size-3.5" />仅看拦截
        </Button>
        <div class="ipam-field">
          <span class="ipam-field__label">自动滚动</span>
          <Switch v-model:checked="autoScroll" size="small" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">缓冲区</span>
          <Select
            v-model:value="bufferSize"
            style="width: 118px"
            :options="BUFFER_OPTIONS"
          />
        </div>
        <div class="ipam-toolbar__spacer"></div>
        <div class="flex items-center gap-2">
          <Button size="small" @click="copyVisible">
            <IconifyIcon icon="lucide:copy" class="mr-1 size-3.5" />复制
          </Button>
          <Button size="small" @click="exportTxt">
            <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />导出
          </Button>
        </div>
      </div>

      <div class="ipam-tail">
        <div class="ipam-tail__head">
          <span class="ipam-tail__dots"><i></i><i></i><i></i></span>
          <span class="ipam-tail__title">tail -f /var/log/ipam/events</span>
          <span class="ipam-toolbar__spacer"></span>
          <span class="ipam-tail__meta">
            {{ filterType ? filterType.toUpperCase() : 'ALL' }} · {{ rows.length }}/{{ bufferSize }}
          </span>
          <span v-if="autoScroll" class="ipam-tail__live">
            <span class="ipam-tail__live-dot"></span>AUTO
          </span>
        </div>

        <div ref="bodyRef" class="ipam-tail__body">
          <div v-if="filtered.length === 0" class="ipam-tail__empty">
            <IconifyIcon icon="lucide:satellite-dish" class="mb-2 size-6" />
            <div v-if="keyword || onlyBlocked">没有匹配当前过滤条件的事件</div>
            <div v-else-if="paused">已暂停显示，恢复后合并缓存事件</div>
            <div v-else>等待事件…（触发 dig 或 DHCP 事件后此处实时滚动）</div>
          </div>
          <div
            v-for="(r, i) in filtered"
            :key="`${r.ts}-${i}`"
            class="ipam-tail__row"
            :data-type="r.type"
            :data-action="r.action"
          >
            <span class="ipam-tail__time">{{ fmtTime(r.ts) }}</span>
            <span class="ipam-tail__badge" :data-type="r.type">
              {{ r.type }}
            </span>
            <span v-if="r.domain" class="ipam-tail__domain">
              <template v-for="(seg, si) in segments(r.domain)" :key="si">
                <mark v-if="seg.hit">{{ seg.text }}</mark>
                <template v-else>{{ seg.text }}</template>
              </template>
            </span>
            <span v-if="r.clientIp" class="ipam-tail__ip">
              <template v-for="(seg, si) in segments(r.clientIp)" :key="si">
                <mark v-if="seg.hit">{{ seg.text }}</mark>
                <template v-else>{{ seg.text }}</template>
              </template>
            </span>
            <span v-if="r.clientMac" class="ipam-tail__mac">
              {{ fmtMac(r.clientMac) }}
            </span>
            <span v-if="r.sip" class="ipam-tail__meta">← {{ r.sip }}</span>
            <span v-if="r.answerIp" class="ipam-tail__ip">→ {{ r.answerIp }}</span>
            <span v-if="r.rcode" class="ipam-tail__rcode">{{ r.rcode }}</span>
            <span v-if="r.action" class="ipam-tail__action">
              {{ r.action }}
            </span>
            <span v-if="r.detail" class="ipam-tail__detail" :title="r.detail">
              <template v-for="(seg, si) in segments(shortDetail(r.detail))" :key="si">
                <mark v-if="seg.hit">{{ seg.text }}</mark>
                <template v-else>{{ seg.text }}</template>
              </template>
            </span>
          </div>
        </div>
      </div>
    </Card>
  </div>
</template>
