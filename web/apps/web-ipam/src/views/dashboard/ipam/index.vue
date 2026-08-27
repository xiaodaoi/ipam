<script setup lang="ts">
import type { CSSProperties } from 'vue';

import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue';

import { Badge, Card, Table, Tag } from 'ant-design-vue';

import { getDashboard, type DashboardOverview } from '#/api/ipam';

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

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
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

    <!-- 池利用率 TopN -->
    <Card title="地址池利用率 TopN">
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