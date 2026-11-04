<script setup lang="ts">
import type { HostMetrics, SystemInfo } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  InputNumber,
  message,
  Popconfirm,
  Tag,
} from 'ant-design-vue';

import { getSystemInfo, getSystemMetrics } from '#/api/ipam';
import { requestClient } from '#/api/request';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

defineOptions({ name: 'SystemRuntime' });

const info = ref<SystemInfo>();
const metrics = ref<HostMetrics>();
const loading = ref(false);
const saving = ref(false);
const restarting = ref(false);
const form = reactive({
  faviconUrl: '',
  logoUrl: '',
  serverPort: 8443,
  siteName: '',
});

const mode = computed(() =>
  import.meta.env.MODE === 'production'
    ? $t('system.runtime.mode.prod')
    : $t('system.runtime.mode.dev'),
);
const modeTagColor = import.meta.env.MODE === 'production' ? 'green' : 'orange';

function fmtUptime(sec?: number): string {
  if (!sec) return '—';
  const d = Math.floor(sec / 86_400);
  const h = Math.floor((sec % 86_400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return $t('system.runtime.uptime.days', { d, h });
  if (h > 0) return $t('system.runtime.uptime.hours', { h, m });
  return $t('system.runtime.uptime.minutes', { m });
}

function fmtDateTime(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString('zh-CN', { hour12: false });
}

const uptimeText = computed(() => fmtUptime(metrics.value?.uptimeSec));

async function load() {
  loading.value = true;
  const [web, sysInfo, hostMetrics] = await Promise.allSettled([
    requestClient.get('/system/webui-settings'),
    getSystemInfo(),
    getSystemMetrics(),
  ]);
  if (web.status === 'fulfilled') {
    const v = web.value as {
      faviconUrl?: string;
      logoUrl?: string;
      serverPort?: number;
      siteName?: string;
    };
    form.faviconUrl = v.faviconUrl ?? '';
    form.logoUrl = v.logoUrl ?? '';
    form.serverPort = Number(v.serverPort) || 8443;
    form.siteName = v.siteName ?? '';
  }
  info.value = sysInfo.status === 'fulfilled' ? sysInfo.value : undefined;
  metrics.value =
    hostMetrics.status === 'fulfilled' ? hostMetrics.value : undefined;
  loading.value = false;
}

async function savePort() {
  if (form.serverPort < 1 || form.serverPort > 65_535) {
    message.warning($t('system.runtime.msg.portRange'));
    return;
  }
  saving.value = true;
  try {
    await requestClient.put('/system/webui-settings', {
      faviconUrl: form.faviconUrl,
      logoUrl: form.logoUrl,
      serverPort: form.serverPort,
      siteName: form.siteName,
    });
    message.success($t('system.runtime.msg.portSaved'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.runtime.msg.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

async function restart() {
  restarting.value = true;
  try {
    await requestClient.post('/system/restart');
    message.success($t('system.runtime.msg.restarted'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.runtime.msg.restartFailed'),
    );
  } finally {
    restarting.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:server-cog"
      :title="$t('system.runtime.page.title')"
      :description="$t('system.runtime.page.desc')"
    >
      <template #actions>
        <RouterLink to="/logs-center/storage">
          <Button size="small">
            <IconifyIcon icon="lucide:database" class="mr-1 size-3.5" />{{
              $t('system.runtime.actions.logStorage')
            }}
          </Button>
        </RouterLink>
        <a href="/metrics" target="_blank" rel="noopener">
          <Button size="small">
            <IconifyIcon icon="lucide:activity" class="mr-1 size-3.5" />{{
              $t('system.runtime.actions.metrics')
            }}
          </Button>
        </a>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.runtime.actions.refresh')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.runtime.stat.port')"
        :value="form.serverPort"
        icon="lucide:plug"
        tone="blue"
        :hint="$t('system.runtime.stat.portHint')"
      />
      <StatCard
        :label="$t('system.runtime.stat.version')"
        :value="info?.version ?? '—'"
        icon="lucide:package"
        tone="cyan"
        :hint="
          info?.goVersion
            ? `Go ${info.goVersion}`
            : $t('system.runtime.stat.versionWait')
        "
      />
      <StatCard
        :label="$t('system.runtime.stat.ready')"
        :value="
          info?.ready
            ? $t('system.runtime.stat.ok')
            : $t('system.runtime.stat.bad')
        "
        icon="lucide:activity"
        :tone="info?.ready ? 'green' : 'red'"
        :hint="
          info?.ready
            ? $t('system.runtime.stat.readyHintOn')
            : $t('system.runtime.stat.readyHintOff')
        "
      />
      <StatCard
        :label="$t('system.runtime.stat.uptime')"
        :value="uptimeText"
        icon="lucide:timer"
        tone="violet"
        :hint="
          metrics
            ? $t('system.runtime.stat.uptimeHint', {
                time: fmtDateTime(metrics.ts),
              })
            : $t('system.runtime.stat.uptimeUnavailable')
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:plug-zap" />{{
            $t('system.runtime.params.title')
          }}
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="modeTagColor">{{ mode }}</Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.runtime.params.port')
          }}</span>
          <InputNumber
            v-model:value="form.serverPort"
            :max="65_535"
            :min="1"
            style="width: 100%"
          />
          <span class="ipam-form-hint">
            {{ $t('system.runtime.params.portHint') }}
          </span>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.runtime.params.version')
          }}</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">{{ info?.version ?? '—' }}</span>
            <span class="ipam-hint">{{ info?.goVersion ?? '' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.runtime.params.serverTime')
          }}</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">{{ fmtDateTime(info?.now) }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.runtime.params.ready')
          }}</span>
          <div class="ipam-service-value">
            <Tag class="m-0" :color="info?.ready ? 'green' : 'red'">
              {{
                info?.ready
                  ? $t('system.runtime.params.readyOk')
                  : $t('system.runtime.params.readyBad')
              }}
            </Tag>
          </div>
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ $t('system.runtime.params.hint') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="savePort">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.runtime.params.savePort')
          }}
        </Button>
      </div>

      <div class="ipam-danger-zone">
        <div class="ipam-danger-zone__body">
          <div class="ipam-danger-zone__title">
            <IconifyIcon icon="lucide:triangle-alert" class="size-4" />{{
              $t('system.runtime.danger.title')
            }}
          </div>
          <div class="ipam-danger-zone__desc">
            {{ $t('system.runtime.danger.desc') }}
          </div>
        </div>
        <Popconfirm
          :title="$t('system.runtime.danger.confirmTitle')"
          :description="$t('system.runtime.danger.confirmDesc')"
          :ok-text="$t('system.runtime.danger.confirmOk')"
          :cancel-text="$t('system.runtime.danger.cancel')"
          @confirm="restart"
        >
          <Button danger :loading="restarting">
            <IconifyIcon icon="lucide:power" class="mr-1 size-3.5" />{{
              $t('system.runtime.danger.restart')
            }}
          </Button>
        </Popconfirm>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:hard-drive" />{{ $t('system.runtime.host.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.runtime.host.hint') }}</span>
      </template>
      <div v-if="metrics" class="ipam-figure-row">
        <div class="ipam-figure">
          <span class="ipam-figure__label">CPU</span>
          <span class="ipam-figure__value">{{ Number(metrics.cpu.usagePct).toFixed(1) }}%</span>
          <span class="ipam-figure__hint">{{
            $t('system.runtime.host.cores', { n: metrics.cpu.cores })
          }}</span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">{{
            $t('system.runtime.host.memory')
          }}</span>
          <span class="ipam-figure__value">{{ Number(metrics.memory.usedPct).toFixed(1) }}%</span>
          <span class="ipam-figure__hint">
            {{ (metrics.memory.usedBytes / 1024 ** 3).toFixed(1) }} / {{ (metrics.memory.totalBytes / 1024 ** 3).toFixed(1) }} GB
          </span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">{{
            $t('system.runtime.host.load1')
          }}</span>
          <span class="ipam-figure__value">{{ Number(metrics.cpu.load1).toFixed(2) }}</span>
          <span class="ipam-figure__hint">5m {{ Number(metrics.cpu.load5).toFixed(2) }}</span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">{{
            $t('system.runtime.host.uptime')
          }}</span>
          <span class="ipam-figure__value">{{ uptimeText }}</span>
        </div>
      </div>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">
{{
          $t('system.runtime.host.emptyTitle')
        }}
</div>
        <div class="ipam-empty__desc">
{{
          $t('system.runtime.host.emptyDesc')
        }}
</div>
      </div>
    </Card>
  </div>
</template>
