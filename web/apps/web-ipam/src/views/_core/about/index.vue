<script setup lang="ts">
import type { HostMetrics, SystemInfo } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Descriptions,
  message,
  Skeleton,
  Tag,
} from 'ant-design-vue';

import { getSystemInfo, getSystemMetrics } from '#/api/ipam';
import MetricRing from '#/components/metric-ring.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

defineOptions({ name: 'About' });

const version = (import.meta.env.VITE_APP_VERSION as string) || '5.7.0';
const mode = computed(() =>
  import.meta.env.MODE === 'production'
    ? $t('system.about.mode.prod')
    : $t('system.about.mode.dev'),
);

const info = ref<SystemInfo>();
const metrics = ref<HostMetrics>();
const loading = ref(false);
const failed = ref(false);

const capabilities = computed(() => [
  {
    desc: $t('system.about.cap.addrDesc'),
    icon: 'lucide:network',
    label: $t('system.about.cap.addr'),
  },
  {
    desc: $t('system.about.cap.dhcpDesc'),
    icon: 'lucide:server',
    label: $t('system.about.cap.dhcp'),
  },
  {
    desc: $t('system.about.cap.dnsDesc'),
    icon: 'lucide:globe',
    label: $t('system.about.cap.dns'),
  },
  {
    desc: $t('system.about.cap.logsDesc'),
    icon: 'lucide:scroll-text',
    label: $t('system.about.cap.logs'),
  },
]);

const stack = computed(() => [
  { name: 'Go + Gin', role: $t('system.about.stack.api') },
  { name: 'ISC Kea', role: $t('system.about.stack.dhcp') },
  { name: 'Unbound', role: $t('system.about.stack.dns') },
  { name: 'ClickHouse', role: $t('system.about.stack.logs') },
  { name: 'Vue 3 + Vben', role: $t('system.about.stack.ui') },
]);

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
  if (d > 0) return $t('system.about.uptime.days', { d, h });
  if (h > 0) return $t('system.about.uptime.hours', { h, m });
  return $t('system.about.uptime.minutes', { m });
}
function fmtDecimal(v?: null | number, digits = 2): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return Number(v).toFixed(digits);
}
function fmtDateTime(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString(undefined, { hour12: false });
}
function toneOfPct(pct: number): 'amber' | 'green' | 'red' {
  if (pct > 90) return 'red';
  if (pct > 75) return 'amber';
  return 'green';
}

const cpuPct = computed(() => Number(metrics.value?.cpu.usagePct ?? 0));
const memPct = computed(() => Number(metrics.value?.memory.usedPct ?? 0));
const rootDisk = computed(
  () =>
    metrics.value?.disks.find((d) => d.mount === '/') ??
    metrics.value?.disks[0],
);
const diskPct = computed(() => Number(rootDisk.value?.usedPct ?? 0));
const ready = computed(() => info.value?.ready);
const readyText = computed(() => {
  if (ready.value === undefined) return $t('system.about.ready.unknown');
  return ready.value
    ? $t('system.about.ready.ok')
    : $t('system.about.ready.bad');
});
const readyTone = computed<'green' | 'red' | 'violet'>(() => {
  if (ready.value === undefined) return 'violet';
  return ready.value ? 'green' : 'red';
});
const readyLabel = computed(() => {
  if (ready.value === undefined) return $t('system.about.ready.labelUnknown');
  return ready.value
    ? $t('system.about.ready.labelOk')
    : $t('system.about.ready.labelBad');
});

async function load() {
  loading.value = true;
  failed.value = false;
  const [i, m] = await Promise.allSettled([
    getSystemInfo(),
    getSystemMetrics(),
  ]);
  info.value = i.status === 'fulfilled' ? i.value : undefined;
  metrics.value = m.status === 'fulfilled' ? m.value : undefined;
  failed.value = i.status !== 'fulfilled';
  loading.value = false;
}

const diagnosis = computed(() =>
  [
    $t('system.about.diag.title', {
      time: fmtDateTime(new Date().toISOString()),
    }),
    $t('system.about.diag.frontend', { mode: mode.value, version }),
    $t('system.about.diag.control', {
      name: info.value?.name ?? '—',
      version: info.value?.version ?? '—',
    }),
    $t('system.about.diag.go', { v: info.value?.goVersion ?? '—' }),
    $t('system.about.diag.ready', { state: readyText.value }),
    $t('system.about.diag.uptime', {
      v: fmtUptime(metrics.value?.uptimeSec),
    }),
    $t('system.about.diag.cpu', {
      cores: metrics.value?.cpu.cores ?? '—',
      l1: fmtDecimal(metrics.value?.cpu.load1),
      l5: fmtDecimal(metrics.value?.cpu.load5),
      l15: fmtDecimal(metrics.value?.cpu.load15),
      pct: fmtPct(cpuPct.value),
    }),
    $t('system.about.diag.mem', {
      pct: fmtPct(memPct.value),
      total: fmtBytes(metrics.value?.memory.totalBytes),
      used: fmtBytes(metrics.value?.memory.usedBytes),
    }),
    ...(metrics.value?.disks ?? []).map((d) =>
      $t('system.about.diag.disk', {
        fs: d.fsType ?? '',
        mount: d.mount,
        pct: fmtPct(d.usedPct),
        total: fmtBytes(d.totalBytes),
        used: fmtBytes(d.usedBytes),
      }),
    ),
  ].join('\n'),
);

async function copyDiagnosis() {
  try {
    await navigator.clipboard.writeText(diagnosis.value);
    message.success($t('system.about.diag.copied'));
  } catch {
    message.warning($t('system.about.diag.copyFail'));
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:copyright"
      :title="$t('system.about.header.title')"
      :description="$t('system.about.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <span
            class="ipam-status-dot"
            :data-state="
              ready === undefined ? 'unknown' : ready ? 'up' : 'down'
            "
          ></span>
          {{ readyLabel }}
        </span>
        <Button size="small" @click="copyDiagnosis">
          <IconifyIcon icon="lucide:clipboard-copy" class="mr-1 size-3.5" />{{
            $t('system.about.header.copy')
          }}
        </Button>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.about.header.refresh')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.about.stat.version')"
        :value="`v${version}`"
        icon="lucide:package"
        tone="blue"
        :hint="$t('system.about.stat.versionHint', { v: info?.version ?? '—' })"
      />
      <StatCard
        :label="$t('system.about.stat.status')"
        :value="readyText"
        icon="lucide:server-cog"
        :tone="readyTone"
        :hint="
          info?.name
            ? $t('system.about.stat.statusHint', {
                go: info.goVersion,
                name: info.name,
              })
            : $t('system.about.stat.statusWait')
        "
      />
      <StatCard
        :label="$t('system.about.stat.uptime')"
        :value="fmtUptime(metrics?.uptimeSec)"
        icon="lucide:timer"
        tone="cyan"
        :hint="
          metrics
            ? $t('system.about.stat.uptimeHint', {
                time: fmtDateTime(metrics.ts),
              })
            : $t('system.about.stat.uptimeUnavailable')
        "
      />
      <StatCard
        :label="$t('system.about.stat.cpu')"
        :value="metrics ? fmtPct(cpuPct) : '—'"
        icon="lucide:cpu"
        :tone="metrics ? toneOfPct(cpuPct) : 'violet'"
        :progress="metrics ? Math.min(100, cpuPct) : undefined"
        :hint="
          metrics
            ? $t('system.about.stat.cpuHint', {
                cores: metrics.cpu.cores,
                load: fmtDecimal(metrics.cpu.load1),
              })
            : $t('system.about.stat.cpuUnavailable')
        "
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
                {{ $t('system.about.hero.title') }}
              </h2>
              <Tag color="blue" class="m-0">v{{ version }}</Tag>
              <Tag class="m-0">{{ mode }}</Tag>
              <Tag v-if="info?.version" class="m-0" color="green">
                {{ $t('system.about.hero.controlPlane', { v: info.version }) }}
              </Tag>
            </div>
            <p class="ipam-hint mt-2">
              {{ $t('system.about.hero.desc') }}
            </p>
          </div>
        </div>
        <div class="ipam-about-hero__meta">
          <div class="ipam-figure">
            <span class="ipam-figure__label">{{
              $t('system.about.meta.serverTime')
            }}</span>
            <span class="ipam-figure__value ipam-mono">{{
              fmtDateTime(info?.now)
            }}</span>
          </div>
          <div class="ipam-figure">
            <span class="ipam-figure__label">{{
              $t('system.about.meta.mode')
            }}</span>
            <span class="ipam-figure__value">{{ mode }}</span>
          </div>
          <div class="ipam-figure">
            <span class="ipam-figure__label">{{
              $t('system.about.meta.license')
            }}</span>
            <span class="ipam-figure__value">{{
              $t('system.about.meta.licenseValue')
            }}</span>
          </div>
        </div>
      </div>
    </Card>

    <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:info" />{{
              $t('system.about.info.title')
            }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ $t('system.about.info.hint') }}</span>
        </template>
        <Descriptions :column="2" bordered size="small">
          <Descriptions.Item :label="$t('system.about.info.product')">
            {{ $t('system.about.info.productValue') }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.about.info.frontend')">
            v{{ version }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.about.info.controlPlane')">
            {{ info?.version ?? '—' }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.about.info.go')">
            {{ info?.goVersion ?? '—' }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.about.info.mode')">
            {{ mode }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.about.info.lang')">
            {{ $t('system.about.info.langValue') }}
          </Descriptions.Item>
          <Descriptions.Item
            :label="$t('system.about.info.serverTime')"
            :span="2"
          >
            <span class="ipam-mono">{{ fmtDateTime(info?.now) }}</span>
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.about.info.license')" :span="2">
            {{ $t('system.about.info.licenseValue') }}
          </Descriptions.Item>
        </Descriptions>
        <div v-if="failed" class="ipam-note mt-3">
          <IconifyIcon
            icon="lucide:triangle-alert"
            class="mt-0.5 size-3.5 shrink-0"
          />
          <span>
            {{ $t('system.about.info.fail', { code: '/system/info' }) }}
          </span>
        </div>
      </Card>

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:activity" />{{
              $t('system.about.host.title')
            }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ $t('system.about.host.hint') }}</span>
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
                <small>
                  {{ $t('system.about.host.cores', { n: metrics.cpu.cores }) }}
                </small>
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
                <span>{{ $t('system.about.host.memory') }}</span>
                <small>
                  {{ fmtBytes(metrics.memory.usedBytes) }} /
                  {{ fmtBytes(metrics.memory.totalBytes) }}
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
                <span>{{
                  rootDisk?.mount ?? $t('system.about.host.rootfs')
                }}</span>
                <small>
                  {{ fmtBytes(rootDisk?.usedBytes) }} /
                  {{ fmtBytes(rootDisk?.totalBytes) }}
                </small>
              </div>
            </div>
          </div>

          <div class="ipam-disk-list">
            <div v-for="d in metrics.disks" :key="d.mount" class="ipam-disk">
              <div class="ipam-disk__head">
                <span class="ipam-disk__mount">{{ d.mount }}</span>
                <span class="ipam-disk__meta"
                  >{{ d.device }} · {{ d.fsType }}</span
                >
                <span class="ipam-disk__size">
                  {{ fmtBytes(d.usedBytes) }} / {{ fmtBytes(d.totalBytes) }}
                </span>
                <span class="ipam-disk__pct">{{ fmtPct(d.usedPct) }}</span>
              </div>
              <div
                class="ipam-progress"
                :data-tone="toneOfPct(Number(d.usedPct) || 0)"
              >
                <i
                  :style="{
                    width: `${Math.min(100, Number(d.usedPct) || 0)}%`,
                  }"
                ></i>
              </div>
            </div>
          </div>

          <div class="ipam-figure-row">
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{
                $t('system.about.host.load1')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtDecimal(metrics.cpu.load1)
              }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{
                $t('system.about.host.load5')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtDecimal(metrics.cpu.load5)
              }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{
                $t('system.about.host.load15')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtDecimal(metrics.cpu.load15)
              }}</span>
            </div>
            <div class="ipam-figure">
              <span class="ipam-figure__label">{{
                $t('system.about.host.uptime')
              }}</span>
              <span class="ipam-figure__value">{{
                fmtUptime(metrics.uptimeSec)
              }}</span>
            </div>
          </div>
        </template>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">
            {{ $t('system.about.host.emptyTitle') }}
          </div>
          <div class="ipam-empty__desc">
            {{ $t('system.about.host.emptyDesc') }}
          </div>
        </div>
      </Card>
    </div>

    <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:sparkles" />{{
              $t('system.about.cap.title')
            }}
          </span>
        </template>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div
            v-for="item in capabilities"
            :key="item.label"
            class="ipam-health-item"
          >
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
            <IconifyIcon icon="lucide:layers" />{{
              $t('system.about.stack.title')
            }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ $t('system.about.stack.hint') }}</span>
        </template>
        <div class="ipam-metric-row">
          <div v-for="item in stack" :key="item.name" class="ipam-metric">
            <div class="ipam-metric__label">{{ item.name }}</div>
            <div class="mt-1 text-sm text-[hsl(var(--foreground))]">
              {{ item.role }}
            </div>
          </div>
        </div>
      </Card>
    </div>
  </div>
</template>

<style scoped>
.about-logo {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 64px;
  height: 64px;
  color: hsl(var(--primary));
  background: linear-gradient(
    135deg,
    hsl(var(--primary) / 16%),
    hsl(var(--primary) / 4%)
  );
  border: 1px solid hsl(var(--primary) / 20%);
  border-radius: 16px;
}

.about-cap-icon {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 10%);
  border-radius: 8px;
}
</style>
