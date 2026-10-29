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

const mode = import.meta.env.MODE === 'production' ? '生产环境' : '开发环境';
const modeTagColor = import.meta.env.MODE === 'production' ? 'green' : 'orange';

function fmtUptime(sec?: number): string {
  if (!sec) return '—';
  const d = Math.floor(sec / 86_400);
  const h = Math.floor((sec % 86_400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d} 天 ${h} 小时`;
  if (h > 0) return `${h} 小时 ${m} 分`;
  return `${m} 分钟`;
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
    message.warning('端口需在 1-65535 之间');
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
    message.success('服务端口已保存，重启控制面后生效');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    saving.value = false;
  }
}

async function restart() {
  restarting.value = true;
  try {
    await requestClient.post('/system/restart');
    message.success('已发起重启，页面将短暂不可用，请稍后刷新');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '重启请求失败');
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
      title="服务与运行"
      description="控制面端口、版本、依赖就绪与重启操作；日志保留与归档在「日志中心 → 存储与归档」维护。"
    >
      <template #actions>
        <RouterLink to="/logs-center/storage">
          <Button size="small">
            <IconifyIcon icon="lucide:database" class="mr-1 size-3.5" />日志存储与归档
          </Button>
        </RouterLink>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="服务端口"
        :value="form.serverPort"
        icon="lucide:plug"
        tone="blue"
        hint="修改后需重启控制面生效"
      />
      <StatCard
        label="控制面版本"
        :value="info?.version ?? '—'"
        icon="lucide:package"
        tone="cyan"
        :hint="info?.goVersion ? `Go ${info.goVersion}` : '等待控制面信息…'"
      />
      <StatCard
        label="依赖就绪"
        :value="info?.ready ? '正常' : '异常'"
        icon="lucide:activity"
        :tone="info?.ready ? 'green' : 'red'"
        :hint="info?.ready ? 'PostgreSQL 等关键依赖可达' : '请检查依赖服务与日志'"
      />
      <StatCard
        label="连续运行"
        :value="uptimeText"
        icon="lucide:timer"
        tone="violet"
        :hint="metrics ? `采样于 ${fmtDateTime(metrics.ts)}` : '宿主指标暂不可达'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:plug-zap" />运行参数
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="modeTagColor">{{ mode }}</Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">服务端口</span>
          <InputNumber
            v-model:value="form.serverPort"
            :max="65_535"
            :min="1"
            style="width: 100%"
          />
          <span class="ipam-form-hint">
            容器部署请同步 `.env` 的 IPAM_PORT 并重建容器
          </span>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">控制面版本</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">{{ info?.version ?? '—' }}</span>
            <span class="ipam-hint">{{ info?.goVersion ?? '' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">服务端时间</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">{{ fmtDateTime(info?.now) }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">依赖就绪</span>
          <div class="ipam-service-value">
            <Tag class="m-0" :color="info?.ready ? 'green' : 'red'">
              {{ info?.ready ? '正常' : '异常 / 未接入' }}
            </Tag>
          </div>
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">端口修改需重启控制面；其他运行参数在对应功能页维护</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="savePort">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存端口
        </Button>
      </div>

      <div class="ipam-danger-zone">
        <div class="ipam-danger-zone__body">
          <div class="ipam-danger-zone__title">
            <IconifyIcon icon="lucide:triangle-alert" class="size-4" />重启控制面服务
          </div>
          <div class="ipam-danger-zone__desc">
            重启期间管理页面与 API 会短暂不可用（约 5–15 秒），DHCP/DNS 数据面不受影响；
            端口等配置修改在重启后生效。
          </div>
        </div>
        <Popconfirm
          title="确认重启服务？"
          description="重启后页面将短暂不可用；端口/设置修改在重启后生效。"
          ok-text="重启"
          cancel-text="取消"
          @confirm="restart"
        >
          <Button danger :loading="restarting">
            <IconifyIcon icon="lucide:power" class="mr-1 size-3.5" />重启服务
          </Button>
        </Popconfirm>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:hard-drive" />宿主资源
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">来自 /system/metrics</span>
      </template>
      <div v-if="metrics" class="ipam-figure-row">
        <div class="ipam-figure">
          <span class="ipam-figure__label">CPU</span>
          <span class="ipam-figure__value">{{ Number(metrics.cpu.usagePct).toFixed(1) }}%</span>
          <span class="ipam-figure__hint">{{ metrics.cpu.cores }} 核</span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">内存</span>
          <span class="ipam-figure__value">{{ Number(metrics.memory.usedPct).toFixed(1) }}%</span>
          <span class="ipam-figure__hint">
            {{ (metrics.memory.usedBytes / 1024 ** 3).toFixed(1) }} / {{ (metrics.memory.totalBytes / 1024 ** 3).toFixed(1) }} GB
          </span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">1 分钟负载</span>
          <span class="ipam-figure__value">{{ Number(metrics.cpu.load1).toFixed(2) }}</span>
          <span class="ipam-figure__hint">5m {{ Number(metrics.cpu.load5).toFixed(2) }}</span>
        </div>
        <div class="ipam-figure">
          <span class="ipam-figure__label">连续运行</span>
          <span class="ipam-figure__value">{{ uptimeText }}</span>
        </div>
      </div>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">宿主资源暂不可达</div>
        <div class="ipam-empty__desc">控制面未挂载宿主 /proc 或指标采集未接线。</div>
      </div>
    </Card>
  </div>
</template>
