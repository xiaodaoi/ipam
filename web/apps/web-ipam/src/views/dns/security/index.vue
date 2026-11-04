<script setup lang="ts">
import type {
  DiagnoseRequestRow,
  DiagnoseResult,
  DnsSettings,
} from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Select,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import { diagnoseDns, getDnsSettings, updateDnsSettings } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const settings = ref<DnsSettings>();
const testName = ref('');
const testType = ref<DiagnoseRequestRow['type']>('A');
const testClientIp = ref('');
const testing = ref(false);
const diag = ref<DiagnoseResult>();
const saving = ref(false);
const lastTestAt = ref('');

async function load() {
  settings.value = await getDnsSettings();
}
async function save() {
  if (!settings.value) return;
  saving.value = true;
  try {
    settings.value = await updateDnsSettings(settings.value);
    message.success($t('dns.security.msg.saved'));
  } finally {
    saving.value = false;
  }
}
const QTYPE_OPTIONS = [
  'A',
  'AAAA',
  'CNAME',
  'MX',
  'NS',
  'TXT',
  'PTR',
  'SOA',
].map((t) => ({ label: t, value: t }));
const RCODE_COLOR: Record<string, string> = {
  NOERROR: 'green',
  NXDOMAIN: 'orange',
  REFUSED: 'red',
  SERVFAIL: 'red',
};

async function runTest() {
  if (!testName.value) {
    message.warning($t('dns.security.msg.needName'));
    return;
  }
  testing.value = true;
  try {
    diag.value = await diagnoseDns({
      name: testName.value,
      type: testType.value,
      clientIp: testClientIp.value || undefined,
    });
    lastTestAt.value = new Date().toLocaleTimeString(undefined, {
      hour12: false,
    });
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.security.msg.queryFailed'),
    );
  } finally {
    testing.value = false;
  }
}
onMounted(load);

const stats = computed(() => {
  const s = settings.value;
  return {
    dnssec: s?.dnssecValidate ?? false,
    rcode: diag.value?.rcode ?? '',
    rrlEnabled: s?.rrlEnabled ?? false,
    rrlRate: s?.rrlRate ?? 0,
    rttMs: diag.value?.rttMs ?? 0,
    tcpOnly: s?.tcpOnly ?? false,
    tested: !!diag.value,
  };
});

const answerColumns = computed(() => [
  { title: $t('dns.security.test.colName'), dataIndex: 'name', minWidth: 240 },
  { title: $t('dns.security.test.colType'), dataIndex: 'type', width: 90 },
  { title: $t('dns.security.test.colTtl'), dataIndex: 'ttl', width: 90 },
  {
    title: $t('dns.security.test.colValue'),
    dataIndex: 'value',
    minWidth: 220,
  },
]);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:shield-check"
      :title="$t('dns.security.header.title')"
      :description="$t('dns.security.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:shield" class="size-3.5" />
          {{
            $t('dns.security.header.meta', {
              rrl: stats.rrlEnabled
                ? $t('dns.common.on')
                : $t('dns.common.off'),
              dnssec: stats.dnssec ? $t('dns.common.on') : $t('dns.common.off'),
            })
          }}
        </span>
        <Button size="small" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dns.common.refresh')
          }}
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          {{ $t('dns.security.actions.save') }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dns.security.stat.rrl')"
        :value="
          stats.rrlEnabled
            ? $t('dns.security.stat.rrlOn')
            : $t('dns.security.stat.rrlOff')
        "
        icon="lucide:gauge"
        :tone="stats.rrlEnabled ? 'green' : 'amber'"
        :hint="
          stats.rrlEnabled
            ? $t('dns.security.stat.rrlHintOn')
            : $t('dns.security.stat.rrlHintOff')
        "
      />
      <StatCard
        :label="$t('dns.security.stat.rate')"
        :value="stats.rrlEnabled ? stats.rrlRate : '—'"
        :unit="stats.rrlEnabled ? $t('dns.security.stat.rateUnit') : ''"
        icon="lucide:gauge"
        tone="blue"
        :hint="
          stats.rrlEnabled
            ? $t('dns.security.stat.rateHintOn')
            : $t('dns.security.stat.rateHintOff')
        "
      />
      <StatCard
        :label="$t('dns.security.stat.dnssec')"
        :value="
          stats.dnssec
            ? $t('dns.security.stat.dnssecOn')
            : $t('dns.security.stat.dnssecOff')
        "
        icon="lucide:badge-check"
        :tone="stats.dnssec ? 'green' : 'violet'"
        :hint="
          stats.dnssec
            ? $t('dns.security.stat.dnssecHintOn')
            : $t('dns.security.stat.dnssecHintOff')
        "
      />
      <StatCard
        :label="$t('dns.security.stat.last')"
        :value="stats.tested ? stats.rcode : $t('dns.security.stat.untested')"
        :unit="stats.tested ? `${stats.rttMs}ms` : ''"
        icon="lucide:stethoscope"
        :tone="
          stats.rcode === 'NOERROR' ? 'cyan' : stats.tested ? 'amber' : 'blue'
        "
        :hint="
          stats.tested
            ? $t('dns.security.stat.lastHint', { time: lastTestAt })
            : $t('dns.security.stat.lastHintOff')
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield" />
          {{ $t('dns.security.policy.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.security.policy.hint') }}</span>
      </template>

      <div v-if="settings" class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.security.form.rrlLabel')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.rrlEnabled" size="small" />
            <span class="ipam-hint">{{
              settings.rrlEnabled
                ? $t('dns.security.form.rrlHintOn')
                : $t('dns.security.form.rrlHintOff')
            }}</span>
          </div>
        </div>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.security.form.rateLabel')
          }}</span>
          <InputNumber
            v-model:value="settings.rrlRate"
            :min="10"
            :disabled="!settings.rrlEnabled"
            style="width: 100%"
          />
          <span class="ipam-hint">{{ $t('dns.security.form.rateHint') }}</span>
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.security.form.dnssecLabel')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.dnssecValidate" size="small" />
            <span class="ipam-hint">{{
              settings.dnssecValidate
                ? $t('dns.security.form.dnssecHintOn')
                : $t('dns.security.form.dnssecHintOff')
            }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.security.form.tcpLabel')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.tcpOnly" size="small" />
            <span class="ipam-hint">{{
              settings.tcpOnly
                ? $t('dns.security.form.tcpHintOn')
                : $t('dns.security.form.tcpHintOff')
            }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.security.form.hint') }}
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.security.blockPage.label')
          }}</span>
          <div class="ipam-form-hint">
            {{ $t('dns.security.blockPage.hint') }}
            <a href="/blocked" target="_blank" rel="noreferrer">{{
              $t('dns.security.blockPage.preview')
            }}</a>
          </div>
        </div>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.security.blockPage.ipLabel')
          }}</span>
          <Input
            v-model:value="settings.blockPageIP"
            :placeholder="$t('dns.security.blockPage.ipPlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.security.blockPage.titleLabel')
          }}</span>
          <Input
            v-model:value="settings.blockPageTitle"
            :maxlength="64"
            :placeholder="$t('dns.security.blockPage.titlePlaceholder')"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.security.blockPage.msgLabel')
          }}</span>
          <Input.TextArea
            v-model:value="settings.blockPageMessage"
            :rows="2"
            :maxlength="500"
            :placeholder="$t('dns.security.blockPage.msgPlaceholder')"
          />
        </label>
        <div class="ipam-form-field--wide flex justify-end">
          <Button type="primary" :loading="saving" @click="save">
            <IconifyIcon icon="lucide:save" class="mr-1" />{{
              $t('dns.security.form.save')
            }}
          </Button>
        </div>
      </div>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">
          {{ $t('dns.security.loading.title') }}
        </div>
        <div class="ipam-empty__desc">
          {{ $t('dns.security.loading.desc') }}
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:terminal-square" />
          {{ $t('dns.security.test.title') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.security.test.hint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.security.test.nameLabel')
          }}</span>
          <Input
            v-model:value="testName"
            :placeholder="$t('dns.security.test.namePlaceholder')"
            style="width: 320px"
            @press-enter="runTest"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.security.test.typeLabel')
          }}</span>
          <Select
            v-model:value="testType"
            style="width: 100px"
            :options="QTYPE_OPTIONS"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.security.test.clientLabel')
          }}</span>
          <Input
            v-model:value="testClientIp"
            :placeholder="$t('dns.security.test.clientPlaceholder')"
            style="width: 220px"
          />
        </div>
        <Button type="primary" size="small" :loading="testing" @click="runTest">
          <IconifyIcon icon="lucide:play" class="mr-1" />{{
            $t('dns.security.test.run')
          }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span v-if="lastTestAt" class="ipam-hint">{{
          $t('dns.security.test.lastQuery', { time: lastTestAt })
        }}</span>
      </div>

      <div v-if="diag">
        <div class="ipam-summary mb-3">
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">{{
              $t('dns.security.test.rcode')
            }}</span>
            <span class="ipam-summary__value">
              <Tag class="m-0" :color="RCODE_COLOR[diag.rcode] ?? 'red'">{{
                diag.rcode
              }}</Tag>
            </span>
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">{{
              $t('dns.security.test.rtt')
            }}</span>
            <span class="ipam-summary__value"
              >{{ diag.rttMs }}<small>ms</small></span
            >
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">{{
              $t('dns.security.test.server')
            }}</span>
            <span class="ipam-summary__value"
              ><span class="ipam-mono text-xs">{{ diag.server }}</span></span
            >
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">{{
              $t('dns.security.test.view')
            }}</span>
            <span class="ipam-summary__value">{{
              diag.viewHint || $t('dns.security.test.viewDefault')
            }}</span>
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">{{
              $t('dns.security.test.answers')
            }}</span>
            <span class="ipam-summary__value"
              >{{ diag.answers.length
              }}<small>{{ $t('dns.common.unit.item') }}</small></span
            >
          </div>
        </div>

        <Table
          v-if="diag.answers.length > 0"
          :data-source="diag.answers.map((a, i) => ({ ...a, key: i }))"
          :columns="answerColumns"
          :pagination="false"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'type'">
              <Tag class="m-0" color="blue">{{ record.type }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'ttl'">
              <span class="ipam-hint">{{ record.ttl ?? '—' }}</span>
            </template>
            <template v-else-if="column.dataIndex === 'value'">
              <span class="ipam-mono text-xs">{{ record.value }}</span>
            </template>
          </template>
        </Table>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">
            {{ $t('dns.security.test.emptyAnswersTitle') }}
          </div>
          <div class="ipam-empty__desc">
            {{
              $t('dns.security.test.emptyAnswersDesc', { rcode: diag.rcode })
            }}
          </div>
        </div>
      </div>
      <div v-else class="ipam-empty">
        <div class="ipam-empty__title">
          {{ $t('dns.security.test.emptyTitle') }}
        </div>
        <div class="ipam-empty__desc">
          {{ $t('dns.security.test.emptyDesc') }}
        </div>
      </div>
    </Card>
  </div>
</template>
