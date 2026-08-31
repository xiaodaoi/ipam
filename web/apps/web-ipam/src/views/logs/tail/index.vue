<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { Card, Select, Tag } from 'ant-design-vue';

import { openLogTail } from '#/api/ipam';

interface TailRow {
  ts: string;
  type: string;
  domain?: string;
  clientMac?: string;
  clientIp?: string;
  action?: string;
  detail?: string;
}

const MAX_ROWS = 200;
const rows = ref<TailRow[]>([]);
const status = ref<'connecting' | 'live' | 'reconnecting'>('connecting');
const filterType = ref<string>();
const received = ref(0);
let es: { close(): void } | undefined;
let timer: ReturnType<typeof setInterval> | undefined;

function start() {
  es?.close();
  status.value = 'connecting';
  es = openLogTail(
    { type: filterType.value },
    {
      onRow: (row: TailRow) => {
        received.value++;
        rows.value = [row, ...rows.value].slice(0, MAX_ROWS);
      },
      onOpen: () => {
        status.value = 'live';
      },
      onError: () => {
        status.value = 'reconnecting';
      },
    },
  );
}

function reload() {
  rows.value = [];
  received.value = 0;
  start();
}
onMounted(start);
onBeforeUnmount(() => {
  es?.close();
  timer && clearInterval(timer);
});

const STATUS_META = {
  connecting: { color: 'processing', text: '连接中' },
  live: { color: 'success', text: '实时中' },
  reconnecting: { color: 'warning', text: '重连中…' },
} as const;

const typeColor = (t?: string) => (t === 'dhcp' ? 'cyan' : 'blue');
const timeStr = (v: string) => new Date(v).toLocaleTimeString();
const filtered = computed(() =>
  filterType.value ? rows.value : rows.value,
);
</script>

<template>
  <Card>
    <template #title>
      <div class="flex items-center gap-3">
        <span>实时日志流</span>
        <Tag :color="STATUS_META[status].color">{{ STATUS_META[status].text }}</Tag>
        <span class="text-xs text-gray-400">已接收 {{ received }} 条 · 保留最近 {{ MAX_ROWS }} 条</span>
      </div>
    </template>
    <template #extra>
      <div class="flex items-center gap-2">
        <Select v-model:value="filterType" style="width: 110px" placeholder="全部" allow-clear
          :options="[{ value: 'dhcp', label: 'DHCP' }, { value: 'dns', label: 'DNS' }]" @change="reload()" />
        <a-button size="small" @click="reload()">重连</a-button>
      </div>
    </template>

    <div class="h-[480px] overflow-auto rounded bg-gray-950 p-3 font-mono text-xs leading-5 text-gray-200">
      <div v-if="!filtered.length" class="text-gray-500">
        等待事件…（触发 dig 或 DHCP 事件后此处实时滚动）
      </div>
      <div v-for="(r, i) in filtered" :key="`${r.ts}-${i}`" class="whitespace-pre-wrap break-all">
        <span class="text-gray-500">{{ timeStr(r.ts) }}</span>
        <Tag :color="typeColor(r.type)" class="mx-1">{{ r.type }}</Tag>
        <template v-if="r.domain"><span class="text-emerald-300">{{ r.domain }}</span> </template>
        <template v-if="r.clientIp"><span class="text-sky-300">{{ r.clientIp }}</span> </template>
        <template v-if="r.clientMac"><span class="text-amber-300">{{ r.clientMac }}</span> </template>
        <span class="text-gray-400">{{ r.action }}</span>
        <span v-if="r.detail" class="text-gray-600"> — {{ r.detail?.slice(0, 90) }}</span>
      </div>
    </div>
  </Card>
</template>