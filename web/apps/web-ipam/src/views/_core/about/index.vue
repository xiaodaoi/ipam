<script setup lang="ts">
import type { HostMetrics, SystemInfo } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Descriptions, message, Skeleton, Tag } from 'ant-design-vue';

import { getSystemInfo, getSystemMetrics } from '#/api/ipam';
import MetricRing from '#/components/metric-ring.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

defineOptions({ name: 'About' });

const version = (import.meta.env.VITE_APP_VERSION as string) || '5.7.0';
const mode = import.meta.env.MODE === 'production' ? '生产环境' : '开发环境';

const info = ref<SystemInfo>();
const metrics = ref<HostMetrics>();
const loading = ref(false);
const failed = ref(false);

const capabilities = [
  { desc: '组织 / 子网 / 地址池与逐地址台账', icon: 'lucide:network', label: '地址空间管理' },
  { desc: 'Kea 配置下发、租约、保留与双栈联动', icon: 'lucide:server', label: 'DHCPv4 / v6' },
  { desc: 'Unbound 上游、转发、记录、缓存与封禁', icon: 'lucide:globe', label: 'DNS 解析' },
  { desc: 'ClickHouse 检索、实时流与操作审计', icon: 'lucide:scroll-text', label: '统一日志' },
];

const stack = [
  { name: 'Go + Gin', role: '控制面 API / 编排' },
  { name: 'ISC Kea', role: 'DHCPv4 / DHCPv6 引擎' },
  { name: 'Unbound', role: '递归 DNS 引擎' },
  { name: 'ClickHouse', role: '日志存储与聚合' },
  { name: 'Vue 3 + Vben', role: '管理界面基座' },
];

function fmtBytes(v?: null | number): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let n = v;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n >= 10 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}
function fmtPct(v?: null | number, digits = 1): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${Number(v).toFixed(digits)}%`;
}
function fmtUptime(sec?: number): string {
  if (!sec) return '—';
  const d = Math.floor(sec / 86_400);
  const h = Math.floor((sec % 86_400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d} 天 ${h} 小时`;
  if (h > 0) return `${h} 小时 ${m} 分`;
  return `${m} 分钟`;
}
function fmtDecimal(v?: null | number, digits = 2): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return Number(v).toFixed(digits);
}
function fmtDateTime(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString('zh-CN', { hour12: false });
}
function toneOfPct(pct: number): 'amber' | 'green' | 'red' {
  if (pct > 90) return 'red';
  if (pct > 75) return 'amber';
  return 'green';
}

const cpuPct = computed(() => Number(metrics.value?.cpu.usagePct ?? 0));
const memPct = computed(() => Number(metrics.value?.memory.usedPct ?? 0));
const rootDisk = computed(
  () => metrics.value?.disks.find((d) => d.mount === '/') ?? metrics.value?.disks[0],
);
const diskPct = computed(() => Number(rootDisk.value?.usedPct ?? 0));
const ready = computed(() => info.value?.ready);
const readyText = computed(() => {
  if (ready.value === undefined) return '未知';
  return ready.value ? '正常' : '异常';
});
const readyTone = computed<'green' | 'red' | 'violet'>(() => {
  if (ready.value === undefined) return 'violet';
  return ready.value ? 'green' : 'red';
});
const readyLabel = computed(() => {
  if (ready.value === undefined) return '控制面状态未知';
  return ready.value ? '控制面运行正常' : '控制面依赖异常';
});

async function load() {
  loading.value = true;
  failed.value = false;
  const [i, m] = await Promise.allSettled([getSystemInfo(), getSystemMetrics()]);
  info.value = i.status === 'fulfilled' ? i.value : undefined;
  metrics.value = m.status === 'fulfilled' ? m.value : undefined;
  failed.value = i.status !== 'fulfilled';
  loading.value = false;
}

const diagnosis = computed(() =>
  [
    `IPAM 诊断信息 · ${fmtDateTime(new Date().toISOString())}`,
    `前端版本: v${version} (${mode})`,
    `控制面: ${info.value?.name ?? '—'} ${info.value?.version ?? '—'}`,
    `Go 运行时: ${info.value?.goVersion ?? '—'}`,
    `依赖就绪: ${readyText.value}`,
    `连续运行: ${fmtUptime(metrics.value?.uptimeSec)}`,
    `CPU: ${fmtPct(cpuPct.value)} · ${metrics.value?.cpu.cores ?? '—'} 核 · 负载 ${fmtDecimal(metrics.value?.cpu.load1)}/${fmtDecimal(metrics.value?.cpu.load5)}/${fmtDecimal(metrics.value?.cpu.load15)}`,
    `内存: ${fmtBytes(metrics.value?.memory.usedBytes)} / ${fmtBytes(metrics.value?.memory.totalBytes)} (${fmtPct(memPct.value)})`,
    ...(metrics.value?.disks ?? []).map(
      (d) =>
        `磁盘 ${d.mount}: ${fmtBytes(d.usedBytes)} / ${fmtBytes(d.totalBytes)} (${fmtPct(d.usedPct)}) · ${d.fsType ?? ''}`,
    ),
  ].join('\n'),
);

async function copyDiagnosis() {
  try {
    await navigator.clipboard.writeText(diagnosis.value);
    message.success('诊断信息已复制到剪贴板');
  } catch {
    message.warning('浏览器未授予剪贴板权限');
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:copyright"
      title="关于平台"
      description="产品版本、运行环境、核心能力与技术栈信息，可一键复制诊断信息用于问题反馈。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <span
            class="ipam-status-dot"
            :data-state="ready === undefined ? 'unknown' : ready ? 'up' : 'down'"
          ></span>
          {{ readyLabel }}
        </span>
        <Button size="small" @click="copyDiagnosis">
          <IconifyIcon icon="lucide:clipboard-copy" class="mr-1 size-3.5" />复制诊断信息
        </Button>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="产品版本"
        :value="`v${version}`"
        icon="lucide:package"
        tone="blue"
        :hint="`控制面 ${info?.version ?? '—'}`"
      />
      <StatCard
        label="控制面状态"
        :value="readyText"
        icon="lucide:server-cog"
        :tone="readyTone"
        :hint="info?.name ? `${info.name} · ${info.goVersion}` : '等待控制面版本信息…'"
      />
      <StatCard
        label="连续运行"
        :value="fmtUptime(metrics?.uptimeSec)"
        icon="lucide:timer"
        tone="cyan"
        :hint="metrics ? `采样于 ${fmtDateTime(metrics.ts)}` : '宿主指标暂不可达'"
      />
      <StatCard
        label="CPU 使用率"
        :value="metrics ? fmtPct(cpuPct) : '—'"
        icon="lucide:cpu"
        :tone="metrics ? toneOfPct(cpuPct) : 'violet'"
        :progress="metrics ? Math.min(100, cpuPct) : undefined"
        :hint="metrics ? `${metrics.cpu.cores} 核 · 1 分钟负载 ${fmtDecimal(metrics.cpu.load1)}` : '未能读取 /system/metrics'"
      />
    </div>

    <Card class="ipam-about-hero">
      <div class="ipam-about-hero__top">
        <div class="ipam-about-hero__main">
          <div class="about-logo">
            <IconifyIcon icon="lucide:network" class="size-8" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="m-0 text-xl font-semibold tracking-tight">
                DDI 统一 IP 地址管理平台
              </h2>
              <Tag color="blue" class="m-0">v{{ version }}</Tag>
              <Tag class="m-0">{{ mode }}</Tag>
              <Tag v-if="info?.version" class="m-0" color="green">
                控制面 {{ info.version }}
              </Tag>
            </div>
            <p class="ipam-hint mt-2">
              IPAM、DHCPv4/v6、DNS 与统一日志一体化产品，面向企业内网提供地址规划、
              自动下发、解析服务与安全审计能力。内网离线可部署，无 GPL 依赖。
            </p>
          </div>
        </div>
        <div class="ipam-about-hero__meta">
        <div class="ipam-figure">
          <span class="ipam-figure__label">服务端时间</span>
          <span class="ipam-figure__value ipam-mono">{{ fmtDateTime(info?.now) }}</span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">运行模式</span>
          <span class="ipam-figure__value">{{ mode }}</span>
        </div>
          <div class="ipam-figure">
            <span class="ipam-figure__label">许可协议</span>
            <span class="ipam-figure__value">商业交付</span>
          </div>
        </div>
      </div>
    </Card>

    <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:info" />产品信息
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">构建期注入版本号</span>
        </template>
        <Descriptions :column="2" bordered size="small">
          <Descriptions.Item label="产品名称">DDI 统一 IP 地址管理平台</Descriptions.Item>
          <Descriptions.Item label="前端版本">v{{ version }}</Descriptions.Item>
          <Descriptions.Item label="控制面版本">
            {{ info?.version ?? '—' }}
          </Descriptions.Item>
          <Descriptions.Item label="Go 运行时">
            {{ info?.goVersion ?? '—' }}
          </Descriptions.Item>
          <Descriptions.Item label="运行模式">{{ mode }}</Descriptions.Item>
          <Descriptions.Item label="界面语言">简体中文</Descriptions.Item>
          <Descriptions.Item label="服务端时间" :span="2">
            <span class="ipam-mono">{{ fmtDateTime(info?.now) }}</span>
          </Descriptions.Item>
          <Descriptions.Item label="许可协议" :span="2">
            商业交付 · 第三方组件许可见《LICENSES.md》
          </Descriptions.Item>
        </Descriptions>
        <div v-if="failed" class="ipam-note mt-3">
          <IconifyIcon icon="lucide:triangle-alert" class="mt-0.5 size-3.5 shrink-0" />
          <span>未能读取 <code>/system/info</code>，控制面版本与状态可能不是最新。</span>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:activity" />宿主资源
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">来自 /system/metrics</span>
        </template>

        <Skeleton v-if="loading && !metrics" active :paragraph="{ rows: 5 }" />
        <template v-else-if="metrics">
          <div class="ipam-gauges">
            <div class="ipam-gauge">
              <MetricRing
                :label="fmtPct(cpuPct)"
                :size="96"
                :thickness="8"
                :tone="toneOfPct(cpuPct)"
                :value="cpuPct"
              />
              <div class="ipam-gauge__caption">
                <span>CPU</span>
                <small>{{ metrics.cpu.cores }} 核</small>
              </div>
            </div>
            <div class="ipam-gauge">
              <MetricRing
                :label="fmtPct(memPct)"
                :size="96"
                :thickness="8"
                :tone="toneOfPct(memPct)"
                :value="memPct"
              />
              <div class="ipam-gauge__caption">
                <span>内存</span>
                <small>
                  {{ fmtBytes(metrics.memory.usedBytes) }} / {{ fmtBytes(metrics.memory.totalBytes) }}
                </small>
              </div>
            </div>
            <div class="ipam-gauge">
              <MetricRing
                :label="fmtPct(diskPct)"
                :size="96"
                :thickness="8"
                :tone="toneOfPct(diskPct)"
                :value="diskPct"
              />
              <div class="ipam-gauge__caption">
                <span>{{ rootDisk?.mount ?? '根分区' }}</span>
                <small>
                  {{ fmtBytes(rootDisk?.usedBytes) }} / {{ fmtBytes(rootDisk?.totalBytes) }}
                </small>
              </div>
            </div>
          </div>

          <div class="ipam-disk-list">
            <div v-for="d in metrics.disks" :key="d.mount" class="ipam-disk">
              <div class="ipam-disk__head">
                <span class="ipam-disk__mount">{{ d.mount }}</span>
                <span class="ipam-disk__meta">{{ d.device }} · {{ d.fsType }}</span>
                <span class="ipam-disk__size">
                  {{ fmtBytes(d.usedBytes) }} / {{ fmtBytes(d.totalBytes) }}
                </span>
                <span class="ipam-disk__pct">{{ fmtPct(d.usedPct) }}</span>
              </div>
              <div class="ipam-progress" :data-tone="toneOfPct(Number(d.usedPct) || 0)">
                <i :style="{ width: `${Math.min(100, Number(d.usedPct) || 0)}%` }"></i>
              </div>
            </div>
          </div>

          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">1 分钟负载</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load1) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">5 分钟负载</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load5) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">15 分钟负载</span>
              <span class="ipam-figure__value">{{ fmtDecimal(metrics.cpu.load15) }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">连续运行</span>
              <span class="ipam-figure__value">{{ fmtUptime(metrics.uptimeSec) }}</span>
            </div>
          </div>
        </template>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">宿主资源暂不可达</div>
          <div class="ipam-empty__desc">
            控制面未挂载宿主 /proc 或指标采集未接线，仅影响本卡片展示。
          </div>
        </div>
      </Card>
    </div>

    <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:sparkles" />核心能力
          </span>
        </template>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div v-for="item in capabilities" :key="item.label" class="ipam-health-item">
            <span class="about-cap-icon">
              <IconifyIcon :icon="item.icon" class="size-4" />
            </span>
            <div class="min-w-0">
              <div class="ipam-health-item__name">{{ item.label }}</div>
              <div class="ipam-hint">{{ item.desc }}</div>
            </div>
          </div>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:layers" />技术栈
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">无 GPL 依赖 · 内网离线可部署</span>
        </template>
        <div class="ipam-metric-row">
          <div v-for="item in stack" :key="item.name" class="ipam-metric">
            <div class="ipam-metric__label">{{ item.name }}</div>
            <div class="mt-1 text-sm text-[hsl(var(--foreground))]">{{ item.role }}</div>
          </div>
        </div>
      </Card>
    </div>
  </div>
</template>

<style scoped>
.about-logo {
  display: flex;
  width: 64px;
  height: 64px;
  flex: none;
  align-items: center;
  justify-content: center;
  border: 1px solid hsl(var(--primary) / 20%);
  border-radius: 16px;
  background: linear-gradient(
    135deg,
    hsl(var(--primary) / 16%),
    hsl(var(--primary) / 4%)
  );
  color: hsl(var(--primary));
}

.about-cap-icon {
  display: flex;
  width: 30px;
  height: 30px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  background: hsl(var(--primary) / 10%);
  color: hsl(var(--primary));
}
</style>
