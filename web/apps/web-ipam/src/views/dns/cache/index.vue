<script setup lang="ts">
import type { DnsSettings } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Popconfirm, Switch, Table } from 'ant-design-vue';

import { flushCache, getDnsSettings, getLogQps, listTtlOverrides, updateDnsSettings, upsertTtlOverride } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const showTotal = (t: number) => $t('dns.common.total', { n: t });

const settings = ref<DnsSettings>();
const ttlRows = ref<{ domain: string; ttl: number }[]>([]);
const newTtl = ref({ domain: '', ttl: 300 });
const flushZone = ref('');
const saving = ref(false);
const flushing = ref(false);
const loading = ref(false);

const qps1h = ref(0);
const qpsTotal = ref(0);
const qpsPeak = ref(0);
const qpsSpark = ref<number[]>([]);

async function load() {
  loading.value = true;
  try {
    settings.value = await getDnsSettings();
    const d = await listTtlOverrides();
    ttlRows.value = (d.items ?? []) as { domain: string; ttl: number }[];
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (!settings.value) return;
  saving.value = true;
  try {
    settings.value = await updateDnsSettings(settings.value);
    message.success($t('dns.cache.msg.saved'));
  } finally {
    saving.value = false;
  }
}
async function doFlush() {
  flushing.value = true;
  try {
    const r = await flushCache(flushZone.value || undefined);
    message.success($t('dns.cache.msg.flushOk', { n: r.flushed, cmd: r.cmd }));
    flushZone.value = '';
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('dns.cache.msg.flushFailed'));
  } finally {
    flushing.value = false;
  }
}
async function addTtl() {
  if (!newTtl.value.domain || !newTtl.value.ttl) {
    message.warning($t('dns.cache.msg.warnDomainTtl'));
    return;
  }
  try {
    await upsertTtlOverride(newTtl.value);
    message.success($t('dns.cache.msg.ttlSaved'));
    newTtl.value = { domain: '', ttl: 300 };
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('dns.common.saveFailed'));
  }
}

async function loadQps() {
  try {
    const from = new Date(Date.now() - 24 * 3_600_000).toISOString();
    const d = await getLogQps({ from, intervalSec: 3600 });
    const pts = d.points ?? [];
    const counts = pts.map((p) => p.count);
    qpsTotal.value = counts.reduce((a, b) => a + b, 0);
    qps1h.value = counts.slice(-1)[0] ?? 0;
    qpsPeak.value = counts.length > 0 ? Math.max(...counts) : 0;
    qpsSpark.value = counts.slice(-12);
  } catch {
    /* QPS 数据不可用时保持 0 */
  }
}
onMounted(() => {
  void load();
  void loadQps();
});

const stats = computed(() => ({
  avgPerHour: qpsTotal.value > 0 ? Math.round(qpsTotal.value / 24) : 0,
  covers: ttlRows.value.length,
  maxTtl: settings.value?.cacheMaxTtl ?? 0,
  minTtl: settings.value?.cacheMinTtl ?? 0,
  peak: qpsPeak.value,
  qps1h: qps1h.value,
  qpsTotal: qpsTotal.value,
  serveExpired: settings.value?.serveExpired ?? false,
}));

const nf = (n: number) => n.toLocaleString('zh-CN');

const ttlCols = computed(() => [
  { title: $t('dns.cache.ttl.colDomain'), dataIndex: 'domain', minWidth: 260 },
  { title: $t('dns.cache.ttl.colTtl'), dataIndex: 'ttl', width: 120 },
  { title: $t('dns.cache.ttl.colHuman'), dataIndex: 'human', width: 140 },
]);

function humanTtl(ttl: number): string {
  if (ttl >= 86_400) return $t('dns.cache.ttl.days', { n: (ttl / 86_400).toFixed(ttl % 86_400 === 0 ? 0 : 1) });
  if (ttl >= 3600) return $t('dns.cache.ttl.hours', { n: (ttl / 3600).toFixed(ttl % 3600 === 0 ? 0 : 1) });
  if (ttl >= 60) return $t('dns.cache.ttl.minutes', { n: (ttl / 60).toFixed(ttl % 60 === 0 ? 0 : 1) });
  return $t('dns.cache.ttl.seconds', { n: ttl });
}
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:gauge"
      :title="$t('dns.cache.header.title')"
      :description="$t('dns.cache.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:database" class="size-3.5" />
          {{ $t('dns.cache.header.meta', { min: stats.minTtl, max: stats.maxTtl, covers: stats.covers }) }}
        </span>
        <Button size="small" :loading="loading" @click="load(); loadQps()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{ $t('dns.common.refresh') }}
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">{{ $t('dns.cache.actions.save') }}</Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dns.cache.stat.qps1h')"
        :value="nf(stats.qps1h)"
        :unit="$t('dns.common.unit.times')"
        icon="lucide:activity"
        tone="blue"
        :spark="qpsSpark.length > 1 ? qpsSpark : undefined"
        :hint="stats.qps1h > 0 ? $t('dns.cache.stat.qps1hHintOn') : $t('dns.cache.stat.qps1hHintOff')"
      />
      <StatCard
        :label="$t('dns.cache.stat.total')"
        :value="nf(stats.qpsTotal)"
        :unit="$t('dns.common.unit.times')"
        icon="lucide:bar-chart-3"
        tone="cyan"
        :hint="$t('dns.cache.stat.totalHint', { n: nf(stats.avgPerHour) })"
      />
      <StatCard
        :label="$t('dns.cache.stat.peak')"
        :value="nf(stats.peak)"
        :unit="$t('dns.common.unit.times')"
        icon="lucide:trending-up"
        tone="violet"
        :hint="stats.peak > 0 ? $t('dns.cache.stat.peakHintOn') : $t('dns.cache.stat.peakHintOff')"
      />
      <StatCard
        :label="$t('dns.cache.stat.covers')"
        :value="stats.covers"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:timer-reset"
        tone="green"
        :hint="stats.covers > 0 ? $t('dns.cache.stat.coversHintOn') : $t('dns.cache.stat.coversHintOff')"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:settings-2" />
          {{ $t('dns.cache.card.params') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.cache.card.hint') }}</span>
      </template>

      <div v-if="settings" class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dns.cache.form.minLabel') }}</span>
          <InputNumber v-model:value="settings.cacheMinTtl" :min="0" style="width: 100%" />
          <span class="ipam-hint">{{ $t('dns.cache.form.minHint') }}</span>
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dns.cache.form.maxLabel') }}</span>
          <InputNumber v-model:value="settings.cacheMaxTtl" :min="1" style="width: 100%" />
          <span class="ipam-hint">{{ $t('dns.cache.form.maxHint') }}</span>
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dns.cache.form.serveExpiredLabel') }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.serveExpired" size="small" />
            <span class="ipam-hint">{{ settings.serveExpired ? $t('dns.cache.form.serveExpiredOn') : $t('dns.cache.form.serveExpiredOff') }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dns.cache.form.policy') }}</span>
          <div class="flex h-8 flex-wrap items-center gap-1">
            <span class="ipam-chip" data-tone="muted">min {{ settings.cacheMinTtl }}s</span>
            <span class="ipam-chip" data-tone="muted">max {{ settings.cacheMaxTtl }}s</span>
            <span class="ipam-chip" :data-tone="settings.serveExpired ? 'green' : 'muted'">
              serve-expired {{ settings.serveExpired ? 'ON' : 'OFF' }}
            </span>
          </div>
        </div>
        <div class="ipam-form-field--wide flex justify-end">
          <Button type="primary" :loading="saving" @click="save">
            <IconifyIcon icon="lucide:save" class="mr-1" />{{ $t('dns.cache.actions.save') }}
          </Button>
        </div>
      </div>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">{{ $t('dns.cache.loading.title') }}</div>
        <div class="ipam-empty__desc">{{ $t('dns.cache.loading.desc') }}</div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:eraser" />
          {{ $t('dns.cache.flush.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">unbound-control flush / flush_zone</span>
      </template>

      <div class="ipam-toolbar">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.cache.flush.zoneLabel') }}</span>
          <Input v-model:value="flushZone" allow-clear :placeholder="$t('dns.cache.flush.zonePlaceholder')" style="width: 280px" />
        </div>
        <Popconfirm
          :title="$t('dns.cache.flush.confirmTitle')"
          :description="flushZone ? $t('dns.cache.flush.confirmDescZone', { zone: flushZone }) : $t('dns.cache.flush.confirmDescAll')"
          :ok-text="$t('dns.cache.flush.ok')"
          :cancel-text="$t('dns.common.cancel')"
          @confirm="doFlush"
        >
          <Button danger :loading="flushing">
            <IconifyIcon icon="lucide:trash-2" class="mr-1" />{{ $t('dns.cache.flush.btn') }}
          </Button>
        </Popconfirm>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dns.cache.flush.hint') }}</span>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:timer-reset" />
          {{ $t('dns.cache.ttl.title') }}
          <em class="ipam-card-sub">{{ $t('dns.cache.ttl.sub', { n: ttlRows.length }) }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.cache.ttl.hint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.cache.ttl.domainLabel') }}</span>
          <Input v-model:value="newTtl.domain" :placeholder="$t('dns.cache.ttl.domainPlaceholder')" style="width: 280px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.cache.ttl.valueLabel') }}</span>
          <InputNumber v-model:value="newTtl.ttl" :min="1" style="width: 140px" />
        </div>
        <Button type="primary" size="small" @click="addTtl">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{ $t('dns.cache.ttl.add') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dns.cache.ttl.toolbarHint') }}</span>
      </div>

      <Table
        v-if="ttlRows.length > 0"
        :data-source="ttlRows"
        :columns="ttlCols"
        :pagination="{ pageSize: 10, showTotal }"
        row-key="domain"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'domain'">
            <span class="ipam-mono text-xs">{{ record.domain }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'ttl'">
            <span class="ipam-chip ipam-mono" data-tone="blue">{{ record.ttl }}s</span>
          </template>
          <template v-else-if="column.dataIndex === 'human'">
            <span class="ipam-hint">{{ humanTtl(record.ttl) }}</span>
          </template>
        </template>
      </Table>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">{{ $t('dns.cache.ttl.emptyTitle') }}</div>
        <div class="ipam-empty__desc">{{ $t('dns.cache.ttl.emptyDesc') }}</div>
      </div>
    </Card>
  </div>
</template>
