<script setup lang="ts">
import type { CSSProperties } from 'vue';

import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue';

import { Badge, Card, Table, Tag } from 'ant-design-vue';

import { getDashboard, getLogQps, type DashboardOverview } from '#/api/ipam';

type Light = 'up' | 'down' | 'unknown';
const LIGHT_TEXT: Record<Light, string> = { up: '正常', down: '异常', unknown: '未接入' };
const LIGHT_LABEL: Record<string, string> = {
  postgres: 'PostgreSQL',
  clickhouse: 'ClickHouse',
  kea: 'Kea DHCP',
  unbound: 'Unbound DNS',
};

const data = ref<DashboardOverview>();
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
  data.value = await getDashboard(5);
  void loadQps();
}
onMounted(() => {
  void load();
  timer = setInterval(load, 30_000);
});
onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
});

const trendMax = computed(() =>
  Math.max(1, ...(data.value?.onlineTrend ?? []).map((p) => p.count)),
);
function barStyle(count: number): CSSProperties {
  return { height: `${Math.round((count / trendMax.value) * 100)}%` };
}

// ── DNS QPS 折线（近 1 小时，60s 分桶）──
const qpsPoints = ref<{ ts: string; count: number }[]>([]);
async function loadQps() {
  const from = new Date(Date.now() - 3600_000).toISOString();
  const to = new Date().toISOString();
  qpsPoints.value = (await getLogQps({ from, to, intervalSec: 60 })).points ?? [];
}
const qpsMax = computed(() => Math.max(1, ...qpsPoints.value.map((p) => p.count)));
const qpsLine = computed(() => {
  const n = qpsPoints.value.length;
  if (!n) return '';
  return qpsPoints.value
    .map((p, i) => {
      const x = (i / (n - 1)) * 600;
      const y = 110 - (p.count / qpsMax.value) * 100;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
});

function fmtPct(v?: number): string {
  if (v === undefined || v === null) return '—';
  return `${v.toFixed(1)}%`;
}
function fmtNum(v?: number | null): string {
  return v === undefined || v === null ? '—' : String(v);
}
</script>

<template>
  <div class="p-4">
    <!-- 活跃终端与趋势 -->
    <Card title="今日活跃终端（租约活跃近似）" class="mb-4">
      <div class="flex flex-wrap items-center gap-8">
        <div class="text-5xl font-semibold tabular-nums">
          {{ data?.onlineNow ?? '—' }}
        </div>
        <div class="flex-1 min-w-[360px]">
          <div class="flex h-24 items-end gap-[3px]">
            <div
              v-for="(p, i) in data?.onlineTrend ?? []"
              :key="i"
              class="min-w-[6px] flex-1 rounded-t bg-blue-400/80"
              :style="barStyle(p.count)"
              :title="`${new Date(p.ts).toLocaleString()} · ${p.count}`"
            ></div>
          </div>
          <div class="mt-1 flex justify-between text-xs text-gray-400">
            <span>-24h</span><span>now</span>
          </div>
        </div>
      </div>
      <div class="mt-4 flex flex-wrap gap-8">
        <div>
          今日新增：
          <b>{{ fmtNum(data?.newTerminals) }}</b>
        </div>
        <div>
          离线终端：
          <b>{{ fmtNum(data?.offlineTerminals) }}</b>
        </div>
        <div>
          联动成功率：
          <b>{{ fmtNum(data?.coherenceSuccessRatePct ?? null) }}%</b>
        </div>
      </div>
    </Card>

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2 mb-4">
      <!-- 服务健康灯 -->
      <Card title="服务健康" class="mb-4">
        <div class="flex flex-wrap gap-x-10 gap-y-3">
          <Badge
            v-for="(label, key) in LIGHT_LABEL"
            :key="key"
            :status="
              ({ up: 'success', down: 'error', unknown: 'warning' } as const)[
                (data?.services?.[key as keyof DashboardOverview['services']] ??
                  'unknown') as Light
              ]
            "
            :text="`${label} · ${LIGHT_TEXT[(data?.services?.[key as keyof DashboardOverview['services']] ?? 'unknown') as Light]}`"
          />
        </div>
        <div class="mt-3 text-xs text-gray-400">
          未配置探测目标或 PoC 内存模式显示「未接入」。
        </div>
      </Card>

      <!-- DNS -->
      <Card title="DNS 服务" class="mb-4">
        <div class="flex flex-wrap gap-10">
          <div>
            近5分钟 QPS
            <div class="text-3xl font-medium">
              {{ data?.dns?.qps5m !== undefined && data?.dns?.qps5m !== null ? (data.dns.qps5m as number).toFixed(1) : '—' }}
            </div>
          </div>
          <div>
            缓存命中率
            <div class="text-3xl font-medium">
              {{ fmtNum(data?.dns?.hitRatePct ?? null) }}
            </div>
            <div class="text-xs text-gray-400">待 unbound 命中日志接入</div>
          </div>
          <div>
            今日拦截
            <div class="text-3xl font-medium">
              {{ fmtNum(data?.dns?.blockedToday ?? null) }}
            </div>
          </div>
        </div>
      </Card>
    </div>

    <!-- DNS QPS 折线（近 1 小时） -->
    <Card title="DNS QPS（近 1 小时 · 折线）" class="mt-4">
      <template #extra>
        <span class="text-xs text-gray-400">60s 分桶 · 30s 自动刷新</span>
      </template>
      <div v-if="qpsPoints.length">
        <svg viewBox="0 0 600 120" class="h-32 w-full" preserveAspectRatio="none">
          <polyline
            :points="qpsLine"
            fill="none"
            stroke="#3b82f6"
            stroke-width="2"
            stroke-linejoin="round"
            stroke-linecap="round"
          />
        </svg>
        <div class="mt-1 flex justify-between text-xs text-gray-400">
          <span>{{ qpsPoints[0] ? new Date(qpsPoints[0]!.ts).toLocaleTimeString() : '' }}</span>
          <span>峰值 {{ qpsMax }} QPS</span>
          <span>now</span>
        </div>
      </div>
      <div v-else class="py-8 text-center text-gray-400">暂无 QPS 数据</div>
    </Card>

    <!-- 池利用率 TopN -->
    <Card title="地址池利用率 TopN" class="mt-4">
      <Table
        :data-source="data?.poolUtilTop ?? []"
        row-key="subnetId"
        size="small"
        :pagination="false"
        :columns="[
          { title: '子网', dataIndex: 'name' },
          { title: 'CIDR', dataIndex: 'cidr' },
          { title: '已用', dataIndex: 'used', width: 90 },
          { title: '容量', dataIndex: 'capacity', width: 110 },
          {
            title: '利用率',
            dataIndex: 'pct',
            width: 120,
            customRender: ({ text }) => h(Tag, {}, () => fmtPct(text)),
          },
        ]"
      />
    </Card>
  </div>
</template>