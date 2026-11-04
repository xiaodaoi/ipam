<script setup lang="ts">
import type { LicenseStatus } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Popconfirm, Tag } from 'ant-design-vue';

import {
  activateLicense,
  getLicenseStatus,
  removeLicense,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

defineOptions({ name: 'SystemLicense' });

const status = ref<LicenseStatus>();
const text = ref('');
const loading = ref(false);
const activating = ref(false);
const removing = ref(false);

const EDITION_LABEL = computed<Record<string, string>>(() => ({
  community: $t('system.license.edition.community'),
  enterprise: $t('system.license.edition.enterprise'),
  lite: $t('system.license.edition.lite'),
  standard: $t('system.license.edition.standard'),
}));

const licensed = computed(() => Boolean(status.value?.licensed));
const edition = computed(
  () =>
    EDITION_LABEL.value[status.value?.edition ?? 'community'] ??
    $t('system.license.edition.community'),
);
const expiry = computed(() => {
  if (!licensed.value) return '—';
  if (!status.value?.expiresAt) return $t('system.license.value.permanent');
  const t = Date.parse(status.value.expiresAt);
  if (Number.isNaN(t)) return '—';
  return new Date(t).toLocaleDateString('zh-CN');
});
const terminalsUsed = computed(() => status.value?.terminalsUsed ?? 0);
const overLimit = computed(() => Boolean(status.value?.terminalsOverLimit));
const maxTerminals = computed(() => status.value?.maxTerminals ?? 0);
const terminal = computed(() => {
  if (!licensed.value) return '—';
  const n = maxTerminals.value;
  if (n <= 0) return $t('system.license.value.unlimited');
  return $t('system.license.stat.terminalsCount', {
    used: terminalsUsed.value,
    total: n,
  });
});
const terminalUsedText = computed(() =>
  licensed.value
    ? $t('system.license.stat.terminalsUsed', { n: terminalsUsed.value })
    : '—',
);
const terminalHint = computed(() => {
  if (!licensed.value) return $t('system.license.stat.terminalsHint');
  if (maxTerminals.value <= 0) {
    return $t('system.license.stat.terminalsUnlimited');
  }
  return overLimit.value
    ? $t('system.license.stat.terminalsOver', {
        n: terminalsUsed.value - maxTerminals.value,
      })
    : $t('system.license.stat.terminalsHint');
});
const days = computed(() => {
  if (!licensed.value) return $t('system.license.value.inactive');
  const n = status.value?.daysRemaining ?? -1;
  if (!status.value?.expiresAt) return $t('system.license.value.permanent');
  return n >= 0
    ? $t('system.license.stat.days', { n })
    : $t('system.license.value.expired');
});

async function load() {
  loading.value = true;
  try {
    status.value = await getLicenseStatus();
    if (status.value?.licensed) {
      text.value = '';
    }
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.license.msg.loadFailed'),
    );
  } finally {
    loading.value = false;
  }
}

async function activate() {
  if (!text.value.trim()) {
    message.warning($t('system.license.msg.pasteRequired'));
    return;
  }
  activating.value = true;
  try {
    status.value = await activateLicense(text.value.trim());
    text.value = '';
    message.success($t('system.license.msg.activated'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.license.msg.activateFailed'),
    );
  } finally {
    activating.value = false;
  }
}

async function remove() {
  removing.value = true;
  try {
    await removeLicense();
    await load();
    message.success($t('system.license.msg.removed'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.license.msg.removeFailed'),
    );
  } finally {
    removing.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:badge-check"
      :title="$t('system.license.page.title')"
      :description="$t('system.license.page.desc')"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.license.actions.refresh')
          }}
        </Button>
        <Popconfirm
          :title="$t('system.license.confirm.removeTitle')"
          :description="$t('system.license.confirm.removeDesc')"
          :ok-text="$t('system.license.confirm.removeOk')"
          :cancel-text="$t('system.license.confirm.cancel')"
          @confirm="remove"
        >
          <Button size="small" danger :loading="removing" :disabled="!licensed">
            {{ $t('system.license.actions.remove') }}
          </Button>
        </Popconfirm>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.license.stat.edition')"
        :value="edition"
        icon="lucide:award"
        :tone="licensed ? 'green' : 'violet'"
        :hint="
          licensed
            ? $t('system.license.stat.editionHintOn')
            : $t('system.license.stat.editionHintOff')
        "
      />
      <StatCard
        :label="$t('system.license.stat.status')"
        :value="
          licensed
            ? $t('system.license.value.valid')
            : $t('system.license.value.unauthorized')
        "
        icon="lucide:shield-check"
        :tone="licensed ? 'green' : 'amber'"
        :hint="status?.licenseId || $t('system.license.stat.statusHint')"
      />
      <StatCard
        :label="$t('system.license.stat.expiry')"
        :value="expiry"
        icon="lucide:calendar-clock"
        tone="cyan"
        :hint="days"
      />
      <StatCard
        :label="$t('system.license.stat.terminals')"
        :value="terminal"
        icon="lucide:monitor-smartphone"
        :tone="overLimit ? 'amber' : 'blue'"
        :hint="terminalHint"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:badge-check" />{{
            $t('system.license.card.infoTitle')
          }}
        </span>
      </template>
      <template #extra>
        <div class="flex items-center gap-2">
          <Tag v-if="overLimit" class="m-0" color="orange">
{{
            $t('system.license.card.overLimit')
          }}
</Tag>
          <Tag class="m-0" :color="licensed ? 'green' : 'default'">
            {{
              licensed
                ? $t('system.license.card.activated')
                : $t('system.license.card.community')
            }}
          </Tag>
        </div>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.license.card.issuedTo')
          }}</span>
          <div class="ipam-service-value">{{ status?.issuedTo || '—' }}</div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.license.card.licenseId')
          }}</span>
          <div class="ipam-service-value ipam-mono">{{ status?.licenseId || '—' }}</div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.license.card.features')
          }}</span>
          <div class="flex flex-wrap gap-1">
            <Tag v-for="f in status?.features ?? []" :key="f" class="m-0">{{ f }}</Tag>
            <span v-if="(status?.features ?? []).length === 0" class="ipam-hint">—</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.license.card.activeTerminals')
          }}</span>
          <div class="ipam-service-value">
            {{ terminalUsedText }}
            <span v-if="maxTerminals > 0" class="ipam-hint">
              {{
                $t('system.license.card.limit', { n: maxTerminals })
              }}
            </span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.license.card.filePath')
          }}</span>
          <div class="ipam-service-value ipam-mono">{{ status?.filePath || '—' }}</div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ status?.message || $t('system.license.card.inactive') }}
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:file-key" />{{
            $t('system.license.activate.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.license.activate.hint') }}</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.license.activate.label')
          }}</span>
          <Input.TextArea
            v-model:value="text"
            :rows="8"
            class="ipam-mono"
            placeholder="payload / signature JSON"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.license.activate.hintText') }}
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ $t('system.license.activate.footerHint') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="activating" @click="activate">
          <IconifyIcon icon="lucide:check" class="mr-1 size-3.5" />{{
            $t('system.license.actions.activate')
          }}
        </Button>
      </div>
    </Card>
  </div>
</template>
