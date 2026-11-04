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
import { $t } from '#/locales';

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
const auto = reactive({
  degraded: false,
  effectiveSamplingMode: '' as '' | LogExportSettings['effectiveSamplingMode'],
  degradedAt: '',
});

function applyStatus(s: LogExportSettings) {
  auto.degraded = Boolean(s.degraded);
  auto.effectiveSamplingMode = s.effectiveSamplingMode ?? '';
  auto.degradedAt = s.degradedAt ?? '';
  if (typeof s.diskUsedPercent === 'number') {
    diskPct.value = s.diskUsedPercent;
  }
}

const samplingLabel = computed(() => {
  if (form.samplingMode === 'all') return $t('logs.export.sampling.all');
  if (form.samplingMode === 'errors') return $t('logs.export.sampling.errors');
  return $t('logs.export.sampling.sample', { n: form.sampleRate });
});
const effectiveLabel = computed(() => {
  if (!auto.degraded) return samplingLabel.value;
  const mode = auto.effectiveSamplingMode;
  if (mode === 'errors') return $t('logs.export.sampling.errorsDegraded');
  if (mode === 'sample') {
    return $t('logs.export.sampling.sampleDegraded', { n: form.sampleRate });
  }
  return samplingLabel.value;
});
const samplingOptions = computed(() => [
  { value: 'all', label: $t('logs.export.samplingCard.modeAll') },
  { value: 'sample', label: $t('logs.export.samplingCard.modeSample') },
  { value: 'errors', label: $t('logs.export.samplingCard.modeErrors') },
]);
const diskTone = computed(() => {
  const p = diskPct.value ?? 0;
  if (p >= 90) return 'red';
  if (p >= 80) return 'amber';
  return 'green';
});
const diskNote = computed(() => {
  const pctText =
    diskPct.value === undefined
      ? $t('logs.export.diskNote.unknown')
      : `${diskPct.value.toFixed(1)}%`;
  const head = $t('logs.export.diskNote.usage', { pct: pctText });
  if (auto.degraded) {
    return (
      head +
      (auto.degradedAt
        ? $t('logs.export.diskNote.degraded', {
            policy: samplingLabel.value,
            time: new Date(auto.degradedAt).toLocaleString(),
          })
        : $t('logs.export.diskNote.degradedNoTime', {
            policy: samplingLabel.value,
          }))
    );
  }
  if ((diskPct.value ?? 0) >= 90)
    return head + $t('logs.export.diskNote.over90');
  if ((diskPct.value ?? 0) >= 85)
    return head + $t('logs.export.diskNote.over85');
  return head + $t('logs.export.diskNote.normal');
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
    applyStatus(s);
    if (diskPct.value === undefined) {
      const m = await getSystemMetrics().catch(() => undefined);
      const root = m?.disks.find((d) => d.mount === '/') ?? m?.disks[0];
      diskPct.value = root ? Number(root.usedPct) : undefined;
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.export.messages.loadFail'),
    );
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    const saved = await updateLogExportSettings({ ...form });
    Object.assign(form, saved);
    applyStatus(saved);
    message.success($t('logs.export.messages.saved'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.export.messages.saveFail'),
    );
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
      :title="$t('logs.export.title')"
      :description="$t('logs.export.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:activity" class="size-3.5" />
          {{
            $t('logs.export.disk', {
              value: diskPct === undefined ? '—' : `${diskPct.toFixed(1)}%`,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('logs.common.refresh')
          }}
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('logs.export.saveApply')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="Syslog"
        :value="
          form.syslogEnabled
            ? $t('logs.export.kpi.enabled')
            : $t('logs.export.kpi.disabled')
        "
        icon="lucide:terminal"
        :tone="form.syslogEnabled ? 'green' : 'violet'"
        :hint="
          form.syslogEnabled
            ? `${form.syslogAddress} · ${form.syslogMode}`
            : $t('logs.export.kpi.syslogHint')
        "
      />
      <StatCard
        label="Kafka"
        :value="
          form.kafkaEnabled
            ? $t('logs.export.kpi.enabled')
            : $t('logs.export.kpi.disabled')
        "
        icon="lucide:git-branch"
        :tone="form.kafkaEnabled ? 'green' : 'violet'"
        :hint="
          form.kafkaEnabled ? form.kafkaTopic : $t('logs.export.kpi.kafkaHint')
        "
      />
      <StatCard
        label="Webhook"
        :value="
          form.webhookEnabled
            ? $t('logs.export.kpi.enabled')
            : $t('logs.export.kpi.disabled')
        "
        icon="lucide:webhook"
        :tone="form.webhookEnabled ? 'green' : 'violet'"
        :hint="
          form.webhookEnabled
            ? form.webhookUrl
            : $t('logs.export.kpi.webhookHint')
        "
      />
      <StatCard
        :label="$t('logs.export.kpi.sampling')"
        :value="effectiveLabel"
        icon="lucide:filter"
        :tone="auto.degraded ? 'amber' : 'cyan'"
        :hint="
          auto.degraded
            ? $t('logs.export.kpi.degradedHint', { policy: samplingLabel })
            : form.samplingMode === 'sample'
              ? $t('logs.export.kpi.samplingHint')
              : form.samplingMode === 'errors'
                ? $t('logs.export.kpi.errorsHint')
                : $t('logs.export.kpi.allHint')
        "
      />
    </div>

    <div class="ipam-note mb-4" :data-tone="diskTone">
      <IconifyIcon icon="lucide:hard-drive" class="mt-0.5 size-3.5 shrink-0" />
      <span>{{ diskNote }}</span>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:send" />{{
            $t('logs.export.channels.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('logs.export.channels.hint') }}</span>
      </template>

      <div class="ipam-channels">
        <!-- Syslog 通道（M6-020 分组排版） -->
        <section class="ipam-channel" :data-on="form.syslogEnabled">
          <header class="ipam-channel__head">
            <span class="ipam-channel__icon">
              <IconifyIcon icon="lucide:terminal" />
            </span>
            <div class="ipam-channel__meta">
              <span class="ipam-channel__title">Syslog</span>
              <span class="ipam-channel__desc">{{
                $t('logs.export.channels.syslogDesc')
              }}</span>
            </div>
            <Switch v-model:checked="form.syslogEnabled" size="small" />
            <span class="ipam-channel__state">{{
              form.syslogEnabled
                ? $t('logs.export.channels.on')
                : $t('logs.export.channels.off')
            }}</span>
          </header>
          <div class="ipam-form-grid ipam-channel__body">
            <div class="ipam-form-field">
              <span class="ipam-form-field__label">{{
                $t('logs.export.channels.address')
              }}</span>
              <Input
                v-model:value="form.syslogAddress"
                :disabled="!form.syslogEnabled"
                placeholder="siem.example.com:514"
              />
            </div>
            <div class="ipam-form-field">
              <span class="ipam-form-field__label">{{
                $t('logs.export.channels.protocol')
              }}</span>
              <Select
                v-model:value="form.syslogMode"
                :disabled="!form.syslogEnabled"
                :options="[
                  { value: 'tcp', label: 'TCP' },
                  { value: 'udp', label: 'UDP' },
                ]"
              />
            </div>
          </div>
        </section>

        <!-- Kafka 通道 -->
        <section class="ipam-channel" :data-on="form.kafkaEnabled">
          <header class="ipam-channel__head">
            <span class="ipam-channel__icon">
              <IconifyIcon icon="lucide:git-branch" />
            </span>
            <div class="ipam-channel__meta">
              <span class="ipam-channel__title">Kafka</span>
              <span class="ipam-channel__desc">{{
                $t('logs.export.channels.kafkaDesc')
              }}</span>
            </div>
            <Switch v-model:checked="form.kafkaEnabled" size="small" />
            <span class="ipam-channel__state">{{
              form.kafkaEnabled
                ? $t('logs.export.channels.on')
                : $t('logs.export.channels.off')
            }}</span>
          </header>
          <div class="ipam-form-grid ipam-channel__body">
            <div class="ipam-form-field">
              <span class="ipam-form-field__label">Brokers</span>
              <Input
                v-model:value="form.kafkaBrokers"
                :disabled="!form.kafkaEnabled"
                placeholder="kafka1:9092,kafka2:9092"
              />
            </div>
            <div class="ipam-form-field">
              <span class="ipam-form-field__label">Topic</span>
              <Input
                v-model:value="form.kafkaTopic"
                :disabled="!form.kafkaEnabled"
                placeholder="ipam-logs"
              />
            </div>
          </div>
        </section>

        <!-- Webhook 通道 -->
        <section class="ipam-channel" :data-on="form.webhookEnabled">
          <header class="ipam-channel__head">
            <span class="ipam-channel__icon">
              <IconifyIcon icon="lucide:webhook" />
            </span>
            <div class="ipam-channel__meta">
              <span class="ipam-channel__title">Webhook</span>
              <span class="ipam-channel__desc">{{
                $t('logs.export.channels.webhookDesc')
              }}</span>
            </div>
            <Switch v-model:checked="form.webhookEnabled" size="small" />
            <span class="ipam-channel__state">{{
              form.webhookEnabled
                ? $t('logs.export.channels.on')
                : $t('logs.export.channels.off')
            }}</span>
          </header>
          <div class="ipam-form-grid ipam-channel__body">
            <div class="ipam-form-field ipam-form-field--wide">
              <span class="ipam-form-field__label">URL</span>
              <Input
                v-model:value="form.webhookUrl"
                :disabled="!form.webhookEnabled"
                placeholder="https://siem.example.com/hooks/ipam"
              />
            </div>
          </div>
        </section>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:filter" />{{
            $t('logs.export.samplingCard.title')
          }}
        </span>
      </template>
      <template #extra>
        <Tag
          class="m-0"
          :color="
            auto.degraded
              ? 'red'
              : form.samplingMode === 'all'
                ? 'green'
                : 'amber'
          "
        >
          {{ effectiveLabel }}
        </Tag>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('logs.export.samplingCard.mode')
          }}</span>
          <Select
            v-model:value="form.samplingMode"
            :options="samplingOptions"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('logs.export.samplingCard.rate')
          }}</span>
          <InputNumber
            v-model:value="form.sampleRate"
            :min="1"
            :max="1000"
            :disabled="form.samplingMode !== 'sample'"
            style="width: 100%"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('logs.export.samplingCard.hint') }}
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{
          $t('logs.export.samplingCard.footer')
        }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('logs.export.saveApply')
          }}
        </Button>
      </div>
    </Card>
  </div>
</template>
