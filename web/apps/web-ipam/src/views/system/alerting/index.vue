<script setup lang="ts">
import type { Dayjs } from 'dayjs';

import type {
  AlertingOverview,
  AlertPolicy,
  AlertPolicyCreate,
  AlertSilence,
  AuditEntry,
  NotificationChannel,
  NotificationChannelCreate,
  NotificationChannelUpdate,
  NotificationDelivery,
  PolicySimulationResult,
} from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  DatePicker,
  Input,
  InputNumber,
  message,
  Modal,
  Select,
  Switch,
  Table,
  TabPane,
  Tabs,
  Tag,
} from 'ant-design-vue';
import dayjs from 'dayjs';

import {
  createAlertPolicy,
  createAlertSilence,
  createNotificationChannel,
  deleteAlertPolicy,
  deleteAlertSilence,
  deleteNotificationChannel,
  getAlertingOverview,
  listAlertPolicies,
  listAlertSilences,
  listAudits,
  listNotificationChannels,
  listNotificationDeliveries,
  simulateAlertPolicy,
  testNotificationChannel,
  updateAlertPolicy,
  updateNotificationChannel,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import TrendChart from '#/components/trend-chart.vue';
import { $t } from '#/locales';

defineOptions({ name: 'SystemAlerting' });

const SCOPE_TYPES = [
  'global',
  'intel_source',
  'blocklist',
  'kind',
  'component',
  'source',
] as const;
const SEVERITIES = ['critical', 'warning', 'info'] as const;
const MODES = ['notify', 'digest', 'silent', 'off'] as const;
const CLASSES = [
  'security_threat',
  'policy_block',
  'system_health',
  'config_health',
] as const;
const GROUPS = ['domain', 'client', 'source', 'none'] as const;
const CHANNEL_TYPES = [
  'webhook',
  'email',
  'feishu',
  'dingtalk',
  'wecom',
  'slack',
  'teams',
] as const;
const KIND_OPTIONS = [
  'risk_url',
  'db_down',
  'logstore_down',
  'dhcp_down',
  'dns_down',
  'disk_pressure',
  'log_export_degraded',
  'intel_sync_failed',
  'upstream_probe_failed',
];

const tab = ref('overview');
const loading = ref(false);
const overview = ref<AlertingOverview>();
const policies = ref<AlertPolicy[]>([]);
const silences = ref<AlertSilence[]>([]);
const channels = ref<NotificationChannel[]>([]);
const deliveries = ref<NotificationDelivery[]>([]);
const audits = ref<AuditEntry[]>([]);

function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString();
}
function fmtPct(v?: number): string {
  return `${((v ?? 0) * 100).toFixed(1)}%`;
}
function fmtDateTime(v?: null | string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  return Number.isNaN(t) || t <= 0
    ? '—'
    : new Date(t).toLocaleString(undefined, { hour12: false });
}
function secondsLabel(v = 0): string {
  if (v <= 0) return $t('alerting.common.unlimited');
  if (v % 3600 === 0) return `${v / 3600}h`;
  if (v % 60 === 0) return `${v / 60}m`;
  return `${v}s`;
}

const scopeOptions = computed(() =>
  SCOPE_TYPES.map((v) => ({ label: $t(`alerting.scope.${v}`), value: v })),
);
const severityOptions = computed(() =>
  SEVERITIES.map((v) => ({
    label: $t(`logs.alerts.severity.${v}`),
    value: v,
  })),
);
const modeOptions = computed(() =>
  MODES.map((v) => ({ label: $t(`alerting.mode.${v}`), value: v })),
);
const classOptions = computed(() =>
  CLASSES.map((v) => ({ label: $t(`alerting.class.${v}`), value: v })),
);
const groupOptions = computed(() =>
  GROUPS.map((v) => ({ label: $t(`alerting.group.${v}`), value: v })),
);
const kindOptions = computed(() =>
  KIND_OPTIONS.map((v) => ({ label: $t(`logs.alerts.kinds.${v}`), value: v })),
);
const channelTypeOptions = computed(() =>
  CHANNEL_TYPES.map((v) => ({
    label: $t(`alerting.channelType.${v}`),
    value: v,
  })),
);

const SEVERITY_COLOR: Record<string, string> = {
  critical: 'red',
  info: 'blue',
  warning: 'orange',
};
const MODE_COLOR: Record<string, string> = {
  digest: 'blue',
  notify: 'green',
  off: 'default',
  silent: 'purple',
};
const CLASS_COLOR: Record<string, string> = {
  config_health: 'cyan',
  policy_block: 'default',
  security_threat: 'volcano',
  system_health: 'red',
};

const trendRecorded = computed(() =>
  (overview.value?.trend ?? []).map((p) => ({ ts: p.ts, value: p.recorded })),
);
const trendNotified = computed(() =>
  (overview.value?.trend ?? []).map((p) => ({ ts: p.ts, value: p.notified })),
);
const topSources = computed(() =>
  (overview.value?.topSources ?? []).toSorted(
    (a, b) => b.recorded - a.recorded,
  ),
);
const topSourceMax = computed(() =>
  Math.max(1, ...topSources.value.map((s) => s.recorded)),
);
const criticalSuppressed = computed(
  () => overview.value?.criticalSuppressed ?? 0,
);

async function loadOverview() {
  const to = new Date();
  const from = new Date(to.getTime() - 24 * 3_600_000);
  overview.value = await getAlertingOverview({
    from: from.toISOString(),
    to: to.toISOString(),
  });
}
async function loadPolicies() {
  const res = await listAlertPolicies();
  policies.value = res.items ?? [];
}
async function loadSilences() {
  const res = await listAlertSilences();
  silences.value = res.items ?? [];
}
async function loadChannels() {
  const res = await listNotificationChannels();
  channels.value = res.items ?? [];
}
async function loadDeliveries() {
  const res = await listNotificationDeliveries({ limit: 30 });
  deliveries.value = res.items ?? [];
}
async function loadAudits() {
  try {
    const to = new Date();
    const from = new Date(to.getTime() - 7 * 24 * 3_600_000);
    const res = await listAudits({
      from: from.toISOString(),
      pageSize: 100,
      q: 'alerting',
      to: to.toISOString(),
    });
    audits.value = res.items ?? [];
  } catch {
    // 审计读取依赖 logs:read；自定义角色可能只有 system:read，静默降级为空。
    audits.value = [];
  }
}

async function refreshAll() {
  loading.value = true;
  try {
    await Promise.all([
      loadOverview(),
      loadPolicies(),
      loadSilences(),
      loadChannels(),
      loadDeliveries(),
      loadAudits(),
    ]);
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
    );
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refreshAll());

// ── 策略 ──
const policyForm = reactive({
  alertClass: 'security_threat' as string,
  enabled: true,
  groupBy: 'domain' as string,
  id: '',
  maxPerHour: 0,
  name: '',
  notifyMode: 'notify' as string,
  priority: 100,
  repeatAfterSec: 86_400,
  resolveAfterSec: 86_400,
  scopeKey: '',
  scopeType: 'intel_source' as string,
  severity: 'warning' as string,
});
const simResult = ref<PolicySimulationResult>();
const simulating = ref(false);

const [PolicyModal, policyModalApi] = useVbenModal({
  class: 'w-[720px]',
  draggable: true,
  onConfirm: () => savePolicy(),
  title: $t('alerting.policyForm.createTitle'),
});

function resetPolicyForm() {
  policyForm.id = '';
  policyForm.name = '';
  policyForm.scopeType = 'intel_source';
  policyForm.scopeKey = '';
  policyForm.priority = 100;
  policyForm.severity = 'warning';
  policyForm.notifyMode = 'notify';
  policyForm.alertClass = 'security_threat';
  policyForm.groupBy = 'domain';
  policyForm.maxPerHour = 0;
  policyForm.repeatAfterSec = 86_400;
  policyForm.resolveAfterSec = 86_400;
  policyForm.enabled = true;
  simResult.value = undefined;
}

function openCreatePolicy() {
  resetPolicyForm();
  policyModalApi.setState({
    title: $t('alerting.policyForm.createTitle'),
  });
  policyModalApi.open();
}

function openEditPolicy(row: Record<string, any>) {
  policyForm.id = row.id;
  policyForm.name = row.name;
  policyForm.scopeType = row.scopeType;
  policyForm.scopeKey = row.scopeKey;
  policyForm.priority = row.priority;
  policyForm.severity = row.severity;
  policyForm.notifyMode = row.notifyMode;
  policyForm.alertClass = row.alertClass;
  policyForm.groupBy = row.groupBy;
  policyForm.maxPerHour = row.maxPerHour;
  policyForm.repeatAfterSec = row.repeatAfterSec;
  policyForm.resolveAfterSec = row.resolveAfterSec;
  policyForm.enabled = row.enabled;
  simResult.value = undefined;
  policyModalApi.setState({ title: $t('alerting.policyForm.editTitle') });
  policyModalApi.open();
}

function policyPayload(): AlertPolicyCreate {
  return {
    alertClass: policyForm.alertClass as AlertPolicyCreate['alertClass'],
    enabled: policyForm.enabled,
    groupBy: policyForm.groupBy as AlertPolicyCreate['groupBy'],
    maxPerHour: policyForm.maxPerHour,
    name: policyForm.name.trim(),
    notifyMode: policyForm.notifyMode as AlertPolicyCreate['notifyMode'],
    priority: policyForm.priority,
    repeatAfterSec: policyForm.repeatAfterSec,
    resolveAfterSec: policyForm.resolveAfterSec,
    scopeKey:
      policyForm.scopeType === 'global'
        ? ''
        : policyForm.scopeKey.trim(),
    scopeType: policyForm.scopeType as AlertPolicyCreate['scopeType'],
    severity: policyForm.severity as AlertPolicyCreate['severity'],
  };
}

function validatePolicyPayload(b: AlertPolicyCreate): boolean {
  if (!b.name || (b.scopeType !== 'global' && !b.scopeKey)) {
    message.warning($t('alerting.policyForm.invalid'));
    return false;
  }
  return true;
}

async function savePolicy() {
  const body = policyPayload();
  if (!validatePolicyPayload(body)) return;
  try {
    if (policyForm.id) {
      await updateAlertPolicy(policyForm.id, body);
      message.success($t('alerting.policies.updated'));
    } else {
      await createAlertPolicy(body);
      message.success($t('alerting.policies.created'));
    }
    policyModalApi.close();
    await Promise.all([loadPolicies(), loadOverview()]);
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
    );
  }
}

async function runSimulation() {
  const body = policyPayload();
  if (!validatePolicyPayload(body)) return;
  simulating.value = true;
  try {
    const to = new Date();
    const from = new Date(to.getTime() - 7 * 24 * 3_600_000);
    simResult.value = await simulateAlertPolicy({
      from: from.toISOString(),
      policy: body,
      to: to.toISOString(),
    });
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
    );
  } finally {
    simulating.value = false;
  }
}

function removePolicy(row: Record<string, any>) {
  Modal.confirm({
    okText: $t('alerting.actions.delete'),
    onOk: async () => {
      try {
        await deleteAlertPolicy(row.id);
        message.success($t('alerting.policies.deleted'));
        await Promise.all([loadPolicies(), loadOverview()]);
      } catch (error) {
        message.error(
          error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
        );
      }
    },
    title: $t('alerting.policies.deleteConfirm', { name: row.name }),
  });
}

async function togglePolicy(row: Record<string, any>, enabled: boolean) {
  try {
    await updateAlertPolicy(row.id, { enabled });
    row.enabled = enabled;
    message.success(
      enabled ? $t('alerting.policies.updated') : $t('alerting.policies.updated'),
    );
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
    );
    await loadPolicies();
  }
}

const policyColumns = computed(() => [
  { dataIndex: 'name', key: 'name', title: $t('alerting.policies.name') },
  { dataIndex: 'scopeType', key: 'scope', title: $t('alerting.policies.scope'), width: 200 },
  { dataIndex: 'severity', key: 'severity', title: $t('alerting.policies.severity'), width: 100 },
  { dataIndex: 'notifyMode', key: 'mode', title: $t('alerting.policies.mode'), width: 120 },
  { dataIndex: 'alertClass', key: 'class', title: $t('alerting.policies.class'), width: 120 },
  { dataIndex: 'groupBy', key: 'group', title: $t('alerting.policies.group'), width: 120 },
  { dataIndex: 'maxPerHour', key: 'cap', title: $t('alerting.policies.maxPerHour'), width: 100 },
  { dataIndex: 'repeatAfterSec', key: 'repeat', title: $t('alerting.policies.repeat'), width: 110 },
  { dataIndex: 'enabled', key: 'enabled', title: $t('alerting.policies.enabled'), width: 90 },
  { key: 'actions', title: '', width: 130 },
]);

// ── 静音 ──
const silenceForm = reactive({
  component: '',
  clientIp: '',
  domain: '',
  endsAt: dayjs().add(1, 'hour') as Dayjs,
  intelSource: '',
  kind: '',
  reason: '',
  severity: '',
  source: '',
  startsAt: dayjs() as Dayjs,
});
const [SilenceModal, silenceModalApi] = useVbenModal({
  class: 'w-[640px]',
  draggable: true,
  onConfirm: () => saveSilence(),
  title: $t('alerting.silences.createTitle'),
});

function openCreateSilence() {
  silenceForm.source = '';
  silenceForm.kind = '';
  silenceForm.component = '';
  silenceForm.intelSource = '';
  silenceForm.severity = '';
  silenceForm.clientIp = '';
  silenceForm.domain = '';
  silenceForm.reason = '';
  silenceForm.startsAt = dayjs();
  silenceForm.endsAt = dayjs().add(1, 'hour');
  silenceModalApi.open();
}

async function saveSilence() {
  if (!silenceForm.endsAt.isAfter(silenceForm.startsAt)) {
    message.warning($t('alerting.silences.invalidWindow'));
    return;
  }
  const matchers = {
    clientIp: silenceForm.clientIp.trim() || undefined,
    component: silenceForm.component.trim() || undefined,
    domain: silenceForm.domain.trim() || undefined,
    intelSource: silenceForm.intelSource.trim() || undefined,
    kind: silenceForm.kind.trim() || undefined,
    severity: silenceForm.severity || undefined,
    source: silenceForm.source.trim() || undefined,
  };
  if (!Object.values(matchers).some(Boolean)) {
    message.warning($t('alerting.silences.atLeastOne'));
    return;
  }
  try {
    await createAlertSilence({
      endsAt: silenceForm.endsAt.toISOString(),
      matchers,
      reason: silenceForm.reason.trim(),
      startsAt: silenceForm.startsAt.toISOString(),
    });
    message.success($t('alerting.silences.created'));
    silenceModalApi.close();
    await Promise.all([loadSilences(), loadOverview()]);
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
    );
  }
}

function removeSilence(row: Record<string, any>) {
  Modal.confirm({
    okText: $t('alerting.actions.delete'),
    onOk: async () => {
      try {
        await deleteAlertSilence(row.id);
        message.success($t('alerting.silences.deleted'));
        await Promise.all([loadSilences(), loadOverview()]);
      } catch (error) {
        message.error(
          error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
        );
      }
    },
    title: $t('alerting.silences.deleteConfirm'),
  });
}

function matcherSummary(m: AlertSilence['matchers']): string {
  const parts: string[] = [];
  const label = (key: string, value?: null | string) => {
    if (value) parts.push(`${$t(`alerting.matchers.${key}`)}=${value}`);
  };
  label('source', m.source);
  label('kind', m.kind);
  label('component', m.component);
  label('intelSource', m.intelSource);
  label('severity', m.severity);
  label('clientIp', m.clientIp);
  label('domain', m.domain);
  return parts.length > 0 ? parts.join(' · ') : $t('alerting.silences.matchAll');
}

const silenceColumns = computed(() => [
  { dataIndex: 'matchers', key: 'matchers', title: $t('alerting.silences.matchers') },
  { dataIndex: 'startsAt', key: 'window', title: $t('alerting.silences.window'), width: 260 },
  { dataIndex: 'reason', key: 'reason', title: $t('alerting.silences.reason'), width: 180 },
  { dataIndex: 'active', key: 'active', title: $t('alerting.silences.active'), width: 90 },
  { key: 'actions', title: '', width: 80 },
]);

// ── 通道 ──
const channelForm = reactive({
  clearSecret: false,
  enabled: true,
  hasSecret: false,
  id: '',
  minSeverity: 'warning' as string,
  name: '',
  quietHours: '',
  recipients: '',
  secret: '',
  smtpFrom: '',
  smtpHost: '',
  smtpPort: 587,
  smtpUser: '',
  type: 'webhook' as string,
  url: '',
});
const [ChannelModal, channelModalApi] = useVbenModal({
  class: 'w-[680px]',
  draggable: true,
  onConfirm: () => saveChannel(),
  title: $t('alerting.channels.createTitle'),
});

function resetChannelForm() {
  channelForm.id = '';
  channelForm.name = '';
  channelForm.type = 'webhook';
  channelForm.url = '';
  channelForm.smtpHost = '';
  channelForm.smtpPort = 587;
  channelForm.smtpUser = '';
  channelForm.smtpFrom = '';
  channelForm.recipients = '';
  channelForm.minSeverity = 'warning';
  channelForm.quietHours = '';
  channelForm.enabled = true;
  channelForm.secret = '';
  channelForm.clearSecret = false;
  channelForm.hasSecret = false;
}

function openCreateChannel() {
  resetChannelForm();
  channelModalApi.setState({ title: $t('alerting.channels.createTitle') });
  channelModalApi.open();
}

function openEditChannel(row: Record<string, any>) {
  resetChannelForm();
  channelForm.id = row.id;
  channelForm.name = row.name;
  channelForm.type = row.type;
  channelForm.url = row.url ?? '';
  channelForm.smtpHost = row.smtpHost ?? '';
  channelForm.smtpPort = row.smtpPort ?? 587;
  channelForm.smtpUser = row.smtpUser ?? '';
  channelForm.smtpFrom = row.smtpFrom ?? '';
  channelForm.recipients = (row.recipients ?? []).join(', ');
  channelForm.minSeverity = row.minSeverity;
  channelForm.quietHours = row.quietHours ?? '';
  channelForm.enabled = row.enabled;
  channelForm.hasSecret = row.hasSecret;
  channelModalApi.setState({ title: $t('alerting.channels.editTitle') });
  channelModalApi.open();
}

function channelCreateBody(): NotificationChannelCreate {
  const email = channelForm.type === 'email';
  return {
    enabled: channelForm.enabled,
    minSeverity: channelForm.minSeverity as NotificationChannelCreate['minSeverity'],
    name: channelForm.name.trim(),
    quietHours: channelForm.quietHours.trim() || undefined,
    recipients: email
      ? channelForm.recipients
          .split(',')
          .map((x) => x.trim())
          .filter(Boolean)
      : undefined,
    secret: channelForm.secret.trim() || undefined,
    smtpFrom: email ? channelForm.smtpFrom.trim() : undefined,
    smtpHost: email ? channelForm.smtpHost.trim() : undefined,
    smtpPort: email ? channelForm.smtpPort : undefined,
    smtpUser: email ? channelForm.smtpUser.trim() : undefined,
    type: channelForm.type as NotificationChannelCreate['type'],
    url: email ? undefined : channelForm.url.trim(),
  };
}

async function saveChannel() {
  if (!channelForm.name.trim()) {
    message.warning($t('alerting.policyForm.invalid'));
    return;
  }
  const body = channelCreateBody();
  try {
    if (channelForm.id) {
      const patch: NotificationChannelUpdate = {
        ...body,
        clearSecret: channelForm.clearSecret,
      };
      await updateNotificationChannel(channelForm.id, patch);
      message.success($t('alerting.channels.updated'));
    } else {
      await createNotificationChannel(body);
      message.success($t('alerting.channels.created'));
    }
    channelModalApi.close();
    await Promise.all([loadChannels(), loadDeliveries(), loadOverview()]);
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
    );
  }
}

function removeChannel(row: Record<string, any>) {
  Modal.confirm({
    okText: $t('alerting.actions.delete'),
    onOk: async () => {
      try {
        await deleteNotificationChannel(row.id);
        message.success($t('alerting.channels.deleted'));
        await Promise.all([loadChannels(), loadDeliveries(), loadOverview()]);
      } catch (error) {
        message.error(
          error instanceof Error ? error.message : $t('alerting.policies.saveFailed'),
        );
      }
    },
    title: $t('alerting.channels.deleteConfirm', { name: row.name }),
  });
}

async function testChannel(row: Record<string, any>) {
  try {
    const res = await testNotificationChannel(row.id);
    if (res.ok) {
      message.success($t('alerting.channels.testOk'));
    } else {
      message.error(
        `${$t('alerting.channels.testFail')}${res.error ? `: ${res.error}` : ''}`,
      );
    }
    await Promise.all([loadChannels(), loadDeliveries()]);
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('alerting.channels.testFail'),
    );
  }
}

const channelColumns = computed(() => [
  { dataIndex: 'name', key: 'name', title: $t('alerting.channels.title') },
  { dataIndex: 'type', key: 'type', title: $t('alerting.channels.type'), width: 110 },
  { dataIndex: 'minSeverity', key: 'minSeverity', title: $t('alerting.channels.minSeverity'), width: 100 },
  { dataIndex: 'quietHours', key: 'quietHours', title: $t('alerting.channels.quietHours'), width: 130 },
  { dataIndex: 'enabled', key: 'enabled', title: $t('alerting.channels.enabled'), width: 80 },
  { dataIndex: 'hasSecret', key: 'hasSecret', title: $t('alerting.channels.hasSecret'), width: 110 },
  { dataIndex: 'lastStatus', key: 'lastStatus', title: $t('alerting.channels.lastStatus'), width: 140 },
  { key: 'actions', title: '', width: 190 },
]);

const deliveryColumns = computed(() => [
  { dataIndex: 'sentAt', key: 'sentAt', title: $t('alerting.deliveries.time'), width: 180 },
  { dataIndex: 'alertId', key: 'alertId', title: $t('alerting.deliveries.alert'), width: 150 },
  { dataIndex: 'channelName', key: 'channelName', title: $t('alerting.deliveries.channel'), width: 160 },
  { dataIndex: 'status', key: 'status', title: $t('alerting.deliveries.status'), width: 100 },
  { dataIndex: 'attempt', key: 'attempt', title: $t('alerting.deliveries.attempt'), width: 80 },
  { dataIndex: 'error', key: 'error', title: $t('alerting.deliveries.error') },
]);

const auditColumns = computed(() => [
  { dataIndex: 'ts', key: 'ts', title: $t('alerting.audit.time'), width: 180 },
  { dataIndex: 'actor', key: 'actor', title: $t('alerting.audit.actor'), width: 130 },
  { dataIndex: 'action', key: 'action', title: $t('alerting.audit.action'), width: 100 },
  { dataIndex: 'method', key: 'method', title: 'Method', width: 90 },
  { dataIndex: 'path', key: 'path', title: $t('alerting.audit.path') },
  { dataIndex: 'status', key: 'status', title: $t('alerting.audit.status'), width: 90 },
]);

const rollupPolicies = computed(() =>
  policies.value.filter(
    (p) => p.enabled && p.scopeType === 'intel_source',
  ),
);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Alert Governance"
      icon="lucide:bell-ring"
      :title="$t('alerting.title')"
      :description="$t('alerting.description')"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="refreshAll">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('alerting.actions.refresh')
          }}
        </Button>
      </template>
    </PageHeader>

    <div v-if="criticalSuppressed > 0" class="ipam-alert-banner">
      <IconifyIcon icon="lucide:triangle-alert" class="size-4" />
      <span>{{ $t('alerting.overview.criticalWarn') }}</span>
      <Tag color="red" class="m-0">{{ fmtInt(criticalSuppressed) }}</Tag>
    </div>

    <Tabs v-model:active-key="tab">
      <TabPane key="overview" :tab="$t('alerting.tabs.overview')">
        <div class="ipam-stat-grid">
          <StatCard
            :label="$t('alerting.overview.recorded')"
            :value="fmtInt(overview?.recorded)"
            icon="lucide:database"
            tone="blue"
            :hint="$t('alerting.overview.trendHint')"
          />
          <StatCard
            :label="$t('alerting.overview.notified')"
            :value="fmtInt(overview?.notified)"
            icon="lucide:bell-ring"
            tone="amber"
            :hint="`${$t('alerting.overview.open')}: ${fmtInt(overview?.open)}`"
          />
          <StatCard
            :label="$t('alerting.overview.archived')"
            :value="fmtInt(overview?.archived)"
            icon="lucide:archive"
            tone="violet"
            :hint="`${$t('alerting.overview.criticalSuppressed')}: ${fmtInt(criticalSuppressed)}`"
          />
          <StatCard
            :label="$t('alerting.overview.noiseRatio')"
            :value="fmtPct(overview?.noiseRatio)"
            icon="lucide:volume-x"
            tone="green"
            :hint="`${$t('alerting.overview.policies')}: ${fmtInt(overview?.policies)} · ${$t('alerting.overview.silences')}: ${fmtInt(overview?.silences)} · ${$t('alerting.overview.channels')}: ${fmtInt(overview?.channels)}`"
          />
        </div>

        <div class="grid items-start gap-4 lg:grid-cols-2">
          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:trending-up" />
                {{ $t('alerting.overview.trendTitle') }}
              </span>
            </template>
            <template #extra>
              <span class="ipam-hint">{{ $t('alerting.overview.recordedShort') }}</span>
            </template>
            <TrendChart
              v-if="trendRecorded.length > 0"
              :data="trendRecorded"
              :height="200"
              :unit="$t('alerting.overview.recordedShort')"
            />
            <div v-else class="ipam-empty">{{ $t('alerting.overview.empty') }}</div>
          </Card>
          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:bell-dot" />
                {{ $t('alerting.overview.trendTitle') }}
              </span>
            </template>
            <template #extra>
              <span class="ipam-hint">{{ $t('alerting.overview.notifiedShort') }}</span>
            </template>
            <TrendChart
              v-if="trendNotified.length > 0"
              :data="trendNotified"
              :height="200"
              tone="amber"
              :unit="$t('alerting.overview.notifiedShort')"
            />
            <div v-else class="ipam-empty">{{ $t('alerting.overview.empty') }}</div>
          </Card>
        </div>

        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:radar" />
              {{ $t('alerting.overview.sourcesTitle') }}
            </span>
          </template>
          <template #extra>
            <span class="ipam-hint">{{ $t('alerting.overview.sourcesHint') }}</span>
          </template>
          <div v-if="topSources.length > 0" class="alert-rank-list">
            <div v-for="(s, idx) in topSources" :key="s.key" class="alert-rank-row">
              <span class="alert-rank-no">{{ idx + 1 }}</span>
              <div class="alert-rank-main">
                <div class="alert-rank-head">
                  <span class="alert-rank-label" :title="s.key">{{ s.key }}</span>
                  <span class="alert-rank-count">
                    {{ fmtInt(s.recorded) }}
                    <small>{{ $t('alerting.overview.recordedShort') }}</small>
                    ·
                    {{ fmtInt(s.notified) }}
                    <small>{{ $t('alerting.overview.notifiedShort') }}</small>
                  </span>
                </div>
                <div class="alert-rank-track">
                  <span :style="{ width: `${(s.recorded / topSourceMax) * 100}%` }"></span>
                </div>
              </div>
            </div>
          </div>
          <div v-else class="ipam-empty">{{ $t('alerting.overview.empty') }}</div>
        </Card>
      </TabPane>

      <TabPane key="policies" :tab="$t('alerting.tabs.policies')">
        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:sliders-horizontal" />
              {{ $t('alerting.policies.title') }}
            </span>
          </template>
          <template #extra>
            <Button type="primary" size="small" @click="openCreatePolicy">
              <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />{{
                $t('alerting.actions.create')
              }}
            </Button>
          </template>
          <p class="ipam-hint mb-3">{{ $t('alerting.policies.subtitle') }}</p>
          <Table
            :columns="policyColumns"
            :data-source="policies"
            :loading="loading"
            :pagination="false"
            :row-key="(r: AlertPolicy) => r.id"
            :scroll="{ x: 1200 }"
            size="middle"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'name'">
                <div class="font-medium">{{ record.name }}</div>
                <div v-if="record.updatedBy" class="ipam-hint">
                  {{ $t('alerting.policies.createdBy') }}: {{ record.updatedBy }}
                </div>
              </template>
              <template v-else-if="column.key === 'scope'">
                <Tag color="geekblue" class="m-0">
                  {{ $t(`alerting.scope.${record.scopeType}`) }}
                </Tag>
                <div class="ipam-hint mt-1">
                  {{ record.scopeKey || $t('alerting.common.none') }}
                </div>
              </template>
              <template v-else-if="column.key === 'severity'">
                <Tag :color="SEVERITY_COLOR[record.severity] ?? 'default'" class="m-0">
                  {{ $t(`logs.alerts.severity.${record.severity}`) }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'mode'">
                <Tag :color="MODE_COLOR[record.notifyMode] ?? 'default'" class="m-0">
                  {{ $t(`alerting.mode.${record.notifyMode}`) }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'class'">
                <Tag :color="CLASS_COLOR[record.alertClass] ?? 'default'" class="m-0">
                  {{ $t(`alerting.class.${record.alertClass}`) }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'group'">
                {{ $t(`alerting.group.${record.groupBy}`) }}
              </template>
              <template v-else-if="column.key === 'cap'">
                {{ record.maxPerHour > 0 ? record.maxPerHour : $t('alerting.common.unlimited') }}
              </template>
              <template v-else-if="column.key === 'repeat'">
                {{ secondsLabel(record.repeatAfterSec) }}
              </template>
              <template v-else-if="column.key === 'enabled'">
                <Switch
                  :checked="record.enabled"
                  size="small"
                  @change="(v: any) => togglePolicy(record, Boolean(v))"
                />
              </template>
              <template v-else-if="column.key === 'actions'">
                <div class="flex gap-1">
                  <Button size="small" type="link" @click="openEditPolicy(record)">
                    {{ $t('alerting.actions.edit') }}
                  </Button>
                  <Button
                    v-if="record.scopeType !== 'global'"
                    size="small"
                    type="link"
                    danger
                    @click="removePolicy(record)"
                  >
                    {{ $t('alerting.actions.delete') }}
                  </Button>
                  <span v-else class="ipam-hint">{{ $t('alerting.policies.globalCannotDelete') }}</span>
                </div>
              </template>
            </template>
            <template #emptyText>
              <div class="ipam-empty">{{ $t('alerting.policies.empty') }}</div>
            </template>
          </Table>
        </Card>
      </TabPane>

      <TabPane key="aggregation" :tab="$t('alerting.tabs.aggregation')">
        <div class="grid items-start gap-4 lg:grid-cols-2">
          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:layers" />
                {{ $t('alerting.aggregation.grouping') }}
              </span>
            </template>
            <p class="ipam-hint">{{ $t('alerting.aggregation.desc') }}</p>
            <div class="ipam-detail-grid">
              <div class="ipam-detail-item">
                <div class="ipam-detail-item__label">{{ $t('alerting.aggregation.grouping') }}</div>
                <div class="ipam-detail-item__value">{{ $t('alerting.aggregation.groupingHint') }}</div>
              </div>
              <div class="ipam-detail-item">
                <div class="ipam-detail-item__label">
                  {{ $t('alerting.aggregation.rollupPreview') }}
                </div>
                <div class="ipam-detail-item__value">
                  <div v-if="rollupPolicies.length > 0" class="flex flex-wrap gap-1">
                    <Tag v-for="p in rollupPolicies" :key="p.id" class="m-0">
                      {{ p.scopeKey }} · {{ $t(`alerting.group.${p.groupBy}`) }}
                    </Tag>
                  </div>
                  <span v-else class="ipam-hint">{{ $t('alerting.common.none') }}</span>
                </div>
              </div>
            </div>
          </Card>
          <Card>
            <template #title>
              <span class="ipam-card-title">
                <IconifyIcon icon="lucide:shield-off" />
                {{ $t('alerting.aggregation.inhibition') }}
              </span>
            </template>
            <p class="ipam-hint">{{ $t('alerting.aggregation.inhibitionHint') }}</p>
            <ul class="alerting-rules">
              <li><IconifyIcon icon="lucide:check" />{{ $t('alerting.aggregation.rule1') }}</li>
              <li><IconifyIcon icon="lucide:check" />{{ $t('alerting.aggregation.rule2') }}</li>
              <li><IconifyIcon icon="lucide:check" />{{ $t('alerting.aggregation.rule3') }}</li>
              <li><IconifyIcon icon="lucide:check" />{{ $t('alerting.aggregation.rule4') }}</li>
            </ul>
          </Card>
        </div>
      </TabPane>

      <TabPane key="silences" :tab="$t('alerting.tabs.silences')">
        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:volume-off" />
              {{ $t('alerting.silences.title') }}
            </span>
          </template>
          <template #extra>
            <Button type="primary" size="small" @click="openCreateSilence">
              <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />{{
                $t('alerting.actions.create')
              }}
            </Button>
          </template>
          <p class="ipam-hint mb-3">{{ $t('alerting.silences.desc') }}</p>
          <Table
            :columns="silenceColumns"
            :data-source="silences"
            :loading="loading"
            :pagination="false"
            :row-key="(r: AlertSilence) => r.id"
            size="middle"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'matchers'">
                <div class="ipam-mono text-xs">{{ matcherSummary(record.matchers) }}</div>
              </template>
              <template v-else-if="column.key === 'window'">
                <div class="text-xs">{{ fmtDateTime(record.startsAt) }}</div>
                <div class="ipam-hint text-xs">→ {{ fmtDateTime(record.endsAt) }}</div>
              </template>
              <template v-else-if="column.key === 'reason'">
                {{ record.reason }}
              </template>
              <template v-else-if="column.key === 'active'">
                <Tag :color="record.active ? 'green' : 'default'" class="m-0">
                  {{ record.active ? $t('alerting.silences.active') : $t('alerting.silences.expired') }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'actions'">
                <Button size="small" type="link" danger @click="removeSilence(record)">
                  {{ $t('alerting.actions.delete') }}
                </Button>
              </template>
            </template>
            <template #emptyText>
              <div class="ipam-empty">{{ $t('alerting.silences.empty') }}</div>
            </template>
          </Table>
        </Card>
      </TabPane>

      <TabPane key="channels" :tab="$t('alerting.tabs.channels')">
        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:send" />
              {{ $t('alerting.channels.title') }}
            </span>
          </template>
          <template #extra>
            <Button type="primary" size="small" @click="openCreateChannel">
              <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />{{
                $t('alerting.actions.create')
              }}
            </Button>
          </template>
          <p class="ipam-hint mb-3">{{ $t('alerting.channels.desc') }}</p>
          <Table
            :columns="channelColumns"
            :data-source="channels"
            :loading="loading"
            :pagination="false"
            :row-key="(r: NotificationChannel) => r.id"
            :scroll="{ x: 1100 }"
            size="middle"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'name'">
                <div class="font-medium">{{ record.name }}</div>
                <div v-if="record.url" class="ipam-hint text-xs">{{ record.url }}</div>
                <div v-else-if="record.smtpHost" class="ipam-hint text-xs">
                  {{ record.smtpHost }}:{{ record.smtpPort }}
                </div>
              </template>
              <template v-else-if="column.key === 'type'">
                <Tag color="geekblue" class="m-0">
                  {{ $t(`alerting.channelType.${record.type}`) }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'minSeverity'">
                <Tag :color="SEVERITY_COLOR[record.minSeverity] ?? 'default'" class="m-0">
                  {{ $t(`logs.alerts.severity.${record.minSeverity}`) }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'enabled'">
                <Tag :color="record.enabled ? 'green' : 'default'" class="m-0">
                  {{ record.enabled ? $t('alerting.common.yes') : $t('alerting.common.no') }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'hasSecret'">
                {{ record.hasSecret ? $t('alerting.common.yes') : $t('alerting.common.no') }}
              </template>
              <template v-else-if="column.key === 'lastStatus'">
                <span class="text-xs">{{ record.lastStatus || '—' }}</span>
              </template>
              <template v-else-if="column.key === 'actions'">
                <div class="flex gap-1">
                  <Button size="small" type="link" @click="openEditChannel(record)">
                    {{ $t('alerting.actions.edit') }}
                  </Button>
                  <Button size="small" type="link" @click="testChannel(record)">
                    {{ $t('alerting.actions.test') }}
                  </Button>
                  <Button
                    size="small"
                    type="link"
                    danger
                    @click="removeChannel(record)"
                  >
                    {{ $t('alerting.actions.delete') }}
                  </Button>
                </div>
              </template>
            </template>
            <template #emptyText>
              <div class="ipam-empty">{{ $t('alerting.channels.empty') }}</div>
            </template>
          </Table>
        </Card>

        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:history" />
              {{ $t('alerting.deliveries.title') }}
            </span>
          </template>
          <template #extra>
            <span class="ipam-hint">{{ $t('alerting.deliveries.desc') }}</span>
          </template>
          <Table
            :columns="deliveryColumns"
            :data-source="deliveries"
            :loading="loading"
            :pagination="false"
            :row-key="(r: NotificationDelivery) => r.id"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'sentAt'">
                <span class="ipam-mono text-xs">{{ fmtDateTime(record.sentAt) }}</span>
              </template>
              <template v-else-if="column.key === 'alertId'">
                <span class="ipam-mono text-xs">{{ String(record.alertId).slice(0, 8) }}…</span>
              </template>
              <template v-else-if="column.key === 'status'">
                <Tag :color="record.status === 'sent' ? 'green' : 'red'" class="m-0">
                  {{ record.status === 'sent'
                    ? $t('alerting.deliveries.sent')
                    : $t('alerting.deliveries.failed') }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'error'">
                <span class="text-xs text-red-500">{{ record.error || '—' }}</span>
              </template>
            </template>
            <template #emptyText>
              <div class="ipam-empty">{{ $t('alerting.deliveries.empty') }}</div>
            </template>
          </Table>
        </Card>
      </TabPane>

      <TabPane key="audit" :tab="$t('alerting.tabs.audit')">
        <Card>
          <template #title>
            <span class="ipam-card-title">
              <IconifyIcon icon="lucide:scroll-text" />
              {{ $t('alerting.audit.title') }}
            </span>
          </template>
          <template #extra>
            <span class="ipam-hint">{{ $t('alerting.audit.desc') }}</span>
          </template>
          <Table
            :columns="auditColumns"
            :data-source="audits"
            :loading="loading"
            :pagination="false"
            :row-key="(r: AuditEntry) => r.id"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'ts'">
                <span class="ipam-mono text-xs">{{ fmtDateTime(record.ts) }}</span>
              </template>
              <template v-else-if="column.key === 'actor'">
                {{ record.actor || record.actorType }}
              </template>
              <template v-else-if="column.key === 'path'">
                <span class="ipam-mono text-xs">{{ record.path }}</span>
              </template>
            </template>
            <template #emptyText>
              <div class="ipam-empty">{{ $t('alerting.audit.empty') }}</div>
            </template>
          </Table>
        </Card>
      </TabPane>
    </Tabs>

    <PolicyModal>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.name') }}</span>
          <Input
            v-model:value="policyForm.name"
            :placeholder="$t('alerting.policyForm.namePlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.scope') }}</span>
          <Select
            v-model:value="policyForm.scopeType"
            :options="scopeOptions"
            :disabled="Boolean(policyForm.id)"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.scopeKey') }}</span>
          <Input
            v-model:value="policyForm.scopeKey"
            :disabled="policyForm.scopeType === 'global' || Boolean(policyForm.id)"
            :placeholder="$t('alerting.policyForm.scopeKeyPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.priority') }}</span>
          <InputNumber v-model:value="policyForm.priority" :min="1" class="w-full" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.severity') }}</span>
          <Select v-model:value="policyForm.severity" :options="severityOptions" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.mode') }}</span>
          <Select v-model:value="policyForm.notifyMode" :options="modeOptions" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.class') }}</span>
          <Select v-model:value="policyForm.alertClass" :options="classOptions" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.group') }}</span>
          <Select v-model:value="policyForm.groupBy" :options="groupOptions" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.maxPerHour') }}</span>
          <InputNumber v-model:value="policyForm.maxPerHour" :min="0" class="w-full" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('alerting.policies.repeat') }} ({{ $t('alerting.policyForm.seconds') }})
          </span>
          <InputNumber v-model:value="policyForm.repeatAfterSec" :min="0" class="w-full" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('alerting.policies.resolve') }} ({{ $t('alerting.policyForm.seconds') }})
          </span>
          <InputNumber v-model:value="policyForm.resolveAfterSec" :min="0" class="w-full" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.policies.enabled') }}</span>
          <div class="flex h-8 items-center">
            <Switch v-model:checked="policyForm.enabled" size="small" />
          </div>
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{ $t('alerting.policyForm.simulateHint') }}</span>
          <div class="flex items-center gap-2">
            <Button size="small" :loading="simulating" @click="runSimulation">
              <IconifyIcon icon="lucide:flask-conical" class="mr-1 size-3.5" />{{
                $t('alerting.actions.simulate')
              }}
            </Button>
            <span v-if="simResult" class="ipam-hint">
              {{ $t('alerting.policyForm.recorded') }}: {{ fmtInt(simResult.recorded) }} ·
              {{ $t('alerting.policyForm.notified') }}: {{ fmtInt(simResult.notified) }} ·
              {{ $t('alerting.policyForm.archived') }}: {{ fmtInt(simResult.archived) }} ·
              {{ $t('alerting.policyForm.criticalSuppressed') }}:
              {{ fmtInt(simResult.criticalSuppressed) }}
            </span>
          </div>
        </div>
      </div>
    </PolicyModal>

    <SilenceModal>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.matchers.source') }}</span>
          <Input v-model:value="silenceForm.source" placeholder="dns / system" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.matchers.kind') }}</span>
          <Select
            v-model:value="silenceForm.kind"
            allow-clear
            show-search
            :options="kindOptions"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.matchers.intelSource') }}</span>
          <Input v-model:value="silenceForm.intelSource" placeholder="stevenblack-hosts" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.matchers.severity') }}</span>
          <Select
            v-model:value="silenceForm.severity"
            allow-clear
            :options="severityOptions"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.matchers.clientIp') }}</span>
          <Input v-model:value="silenceForm.clientIp" placeholder="10.0.0.1" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.matchers.domain') }}</span>
          <Input v-model:value="silenceForm.domain" placeholder="example.com" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.silences.startsAt') }}</span>
          <DatePicker
            v-model:value="silenceForm.startsAt"
            class="w-full"
            format="YYYY-MM-DD HH:mm"
            show-time
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.silences.endsAt') }}</span>
          <DatePicker
            v-model:value="silenceForm.endsAt"
            class="w-full"
            format="YYYY-MM-DD HH:mm"
            show-time
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{ $t('alerting.silences.reason') }}</span>
          <Input
            v-model:value="silenceForm.reason"
            :placeholder="$t('alerting.silences.reasonPlaceholder')"
          />
        </div>
      </div>
    </SilenceModal>

    <ChannelModal>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.title') }}</span>
          <Input v-model:value="channelForm.name" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.type') }}</span>
          <Select
            v-model:value="channelForm.type"
            :disabled="Boolean(channelForm.id)"
            :options="channelTypeOptions"
          />
        </div>
        <template v-if="channelForm.type === 'email'">
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{ $t('alerting.channels.smtpHost') }}</span>
            <Input v-model:value="channelForm.smtpHost" placeholder="smtp.example.com" />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{ $t('alerting.channels.smtpPort') }}</span>
            <InputNumber v-model:value="channelForm.smtpPort" :min="1" class="w-full" />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{ $t('alerting.channels.smtpUser') }}</span>
            <Input v-model:value="channelForm.smtpUser" />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{ $t('alerting.channels.smtpFrom') }}</span>
            <Input v-model:value="channelForm.smtpFrom" placeholder="alerts@example.com" />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">{{ $t('alerting.channels.recipients') }}</span>
            <Input
              v-model:value="channelForm.recipients"
              :placeholder="$t('alerting.channels.recipientsPlaceholder')"
            />
          </div>
        </template>
        <div v-else class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.url') }}</span>
          <Input
            v-model:value="channelForm.url"
            :placeholder="$t('alerting.channels.urlPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.minSeverity') }}</span>
          <Select v-model:value="channelForm.minSeverity" :options="severityOptions" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.quietHours') }}</span>
          <Input
            v-model:value="channelForm.quietHours"
            :placeholder="$t('alerting.channels.quietPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.enabled') }}</span>
          <div class="flex h-8 items-center">
            <Switch v-model:checked="channelForm.enabled" size="small" />
          </div>
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">
            {{ $t('alerting.channels.hasSecret') }}
          </span>
          <Input.Password
            v-model:value="channelForm.secret"
            :placeholder="$t('alerting.channels.secretPlaceholder')"
          />
          <div class="ipam-form-hint">{{ $t('alerting.channels.secretHint') }}</div>
        </div>
        <div v-if="channelForm.id && channelForm.hasSecret" class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('alerting.channels.clearSecret') }}</span>
          <div class="flex h-8 items-center">
            <Switch v-model:checked="channelForm.clearSecret" size="small" />
          </div>
        </div>
      </div>
    </ChannelModal>
  </div>
</template>

<style scoped>
.alerting-rules {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 0;
  margin: 12px 0 0;
  list-style: none;
}

.alerting-rules li {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  font-size: 12.5px;
  line-height: 1.6;
  color: hsl(var(--foreground));
}

.alerting-rules svg {
  flex-shrink: 0;
  width: 14px;
  height: 14px;
  margin-top: 2px;
  color: hsl(var(--primary));
}

.ipam-alert-banner {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 10px 14px;
  margin-bottom: 12px;
  font-size: 12.5px;
  color: #b45309;
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: 10px;
}
</style>
