<script setup lang="ts">
import type { LogExportSettings } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Select,
  Switch,
  Tag,
} from 'ant-design-vue';

import {
  getLogExportSettings,
  getSystemMetrics,
  updateLogExportSettings,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

defineOptions({ name: 'LogExport' });

const form = reactive<LogExportSettings>({
  syslogEnabled: false,
  syslogAddress: '',
  syslogMode: 'tcp',
  kafkaEnabled: false,
  kafkaBrokers: '',
  kafkaTopic: '',
  webhookEnabled: false,
  webhookUrl: '',
  samplingMode: 'all',
  sampleRate: 10,
});
const loading = ref(false);
const saving = ref(false);
const diskPct = ref<number>();

const samplingLabel = computed(() => {
  if (form.samplingMode === 'all') return '全量';
  if (form.samplingMode === 'errors') return '仅异常';
  return `1/${form.sampleRate}`;
});
const diskTone = computed(() => {
  const p = diskPct.value ?? 0;
  if (p >= 90) return 'red';
  if (p >= 80) return 'amber';
  return 'green';
});

async function load() {
  loading.value = true;
  try {
    const s = await getLogExportSettings();
    Object.assign(form, s, {
      syslogMode: s.syslogMode ?? 'tcp',
      samplingMode: s.samplingMode ?? 'all',
      sampleRate: s.sampleRate || 10,
    });
    const m = await getSystemMetrics().catch(() => undefined);
    const root = m?.disks.find((d) => d.mount === '/') ?? m?.disks[0];
    diskPct.value = root ? Number(root.usedPct) : undefined;
  } catch (error) {
    message.error(error instanceof Error ? error.message : '策略加载失败');
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    const saved = await updateLogExportSettings({ ...form });
    Object.assign(form, saved);
    message.success('已保存，Vector 将自动重载配置');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:send"
      title="外发与采样"
      description="将统一日志外发到 Syslog / Kafka / Webhook（SIEM），并按容量策略选择全量、抽样或仅异常。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:activity" class="size-3.5" />
          磁盘 {{ diskPct === undefined ? '—' : `${diskPct.toFixed(1)}%` }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存并应用
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="Syslog"
        :value="form.syslogEnabled ? '已启用' : '未启用'"
        icon="lucide:terminal"
        :tone="form.syslogEnabled ? 'green' : 'violet'"
        :hint="form.syslogEnabled ? `${form.syslogAddress} · ${form.syslogMode}` : '标准 RFC5424 外发'"
      />
      <StatCard
        label="Kafka"
        :value="form.kafkaEnabled ? '已启用' : '未启用'"
        icon="lucide:git-branch"
        :tone="form.kafkaEnabled ? 'green' : 'violet'"
        :hint="form.kafkaEnabled ? form.kafkaTopic : '面向 SIEM/数据平台'"
      />
      <StatCard
        label="Webhook"
        :value="form.webhookEnabled ? '已启用' : '未启用'"
        icon="lucide:webhook"
        :tone="form.webhookEnabled ? 'green' : 'violet'"
        :hint="form.webhookEnabled ? form.webhookUrl : 'JSON over HTTP'"
      />
      <StatCard
        label="采样策略"
        :value="samplingLabel"
        icon="lucide:filter"
        tone="cyan"
        :hint="form.samplingMode === 'sample' ? '按 1/N 抽样写入/外发' : form.samplingMode === 'errors' ? '仅非 INFO 日志' : '全量采集'"
      />
    </div>

    <div class="ipam-note mb-4" :data-tone="diskTone">
      <IconifyIcon icon="lucide:hard-drive" class="mt-0.5 size-3.5 shrink-0" />
      <span>
        当前根分区使用率 {{ diskPct === undefined ? '未知' : `${diskPct.toFixed(1)}%` }}；
        <template v-if="(diskPct ?? 0) >= 90">已超过 90%，建议立即切换为抽样/仅异常并清理归档。</template>
        <template v-else-if="(diskPct ?? 0) >= 80">超过 80%，请关注容量并准备清理归档。</template>
        <template v-else>容量正常。</template>
        磁盘水位自动降级将在后续版本接入，当前需人工切换策略。
      </span>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:send" />外发通道
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">保存后 Vector --watch-config 自动热加载</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Syslog</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.syslogEnabled" size="small" />
            <span class="ipam-hint">{{ form.syslogEnabled ? '启用' : '关闭' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Syslog 地址</span>
          <Input v-model:value="form.syslogAddress" :disabled="!form.syslogEnabled" placeholder="siem.example.com:514" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Syslog 协议</span>
          <Select
            v-model:value="form.syslogMode"
            :disabled="!form.syslogEnabled"
            :options="[{ value: 'tcp', label: 'TCP' }, { value: 'udp', label: 'UDP' }]"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Kafka</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.kafkaEnabled" size="small" />
            <span class="ipam-hint">{{ form.kafkaEnabled ? '启用' : '关闭' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Kafka Brokers</span>
          <Input v-model:value="form.kafkaBrokers" :disabled="!form.kafkaEnabled" placeholder="kafka1:9092,kafka2:9092" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Kafka Topic</span>
          <Input v-model:value="form.kafkaTopic" :disabled="!form.kafkaEnabled" placeholder="ipam-logs" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Webhook</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.webhookEnabled" size="small" />
            <span class="ipam-hint">{{ form.webhookEnabled ? '启用' : '关闭' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">Webhook URL</span>
          <Input v-model:value="form.webhookUrl" :disabled="!form.webhookEnabled" placeholder="https://siem.example.com/hooks/ipam" />
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:filter" />采样与容量
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="form.samplingMode === 'all' ? 'green' : 'amber'">{{ samplingLabel }}</Tag>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">采样模式</span>
          <Select
            v-model:value="form.samplingMode"
            :options="[
              { value: 'all', label: '全量（推荐）' },
              { value: 'sample', label: '按比例抽样' },
              { value: 'errors', label: '仅异常（非 INFO）' },
            ]"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">抽样比例 1/N</span>
          <InputNumber v-model:value="form.sampleRate" :min="1" :max="1000" :disabled="form.samplingMode !== 'sample'" style="width: 100%" />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          采样同时作用于 ClickHouse 入库与全部外发通道；磁盘 80% 预警、90% 建议降级。
          生产切换请评估审计/合规留存要求。
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">外发失败由 Vector 内部重试，不阻塞 ClickHouse 入库</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存并应用
        </Button>
      </div>
    </Card>
  </div>
</template>
