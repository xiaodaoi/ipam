<script setup lang="ts">
import type { Dayjs } from 'dayjs';

import type { AuditEntry, AuditQuery } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';

import { useVbenDrawer } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  DatePicker,
  Input,
  message,
  Select,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import { listAudits, verifyAudits } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const RANGE_HOURS = [
  { hours: 24, key: '24h' },
  { hours: 168, key: '7d' },
  { hours: 720, key: '30d' },
] as const;
type PresetKey = 'custom' | (typeof RANGE_HOURS)[number]['key'];

const ranges = computed(() =>
  RANGE_HOURS.map((r) => ({
    ...r,
    label: $t(`logs.common.range.${r.key}`),
  })),
);

const ACTION_OPTIONS = computed(() => [
  { label: $t('logs.audit.actionOptions.create'), value: 'create' },
  { label: $t('logs.audit.actionOptions.update'), value: 'update' },
  { label: $t('logs.audit.actionOptions.delete'), value: 'delete' },
  { label: $t('logs.audit.actionOptions.login'), value: 'login' },
  { label: $t('logs.audit.actionOptions.logout'), value: 'logout' },
]);
const ACTOR_META = computed<Record<string, { color: string; label: string }>>(
  () => ({
    human: { color: 'blue', label: $t('logs.audit.filters.human') },
    bot: { color: 'purple', label: $t('logs.audit.filters.bot') },
    system: { color: 'default', label: $t('logs.audit.filters.system') },
  }),
);
const ACTION_COLOR: Record<string, string> = {
  create: 'green',
  delete: 'red',
  update: 'blue',
  login: 'cyan',
  logout: 'default',
};

const preset = ref<PresetKey>('7d');
const customRange = ref<[Dayjs, Dayjs]>();
const window = ref({ from: '', to: '', labelKey: 'logs.common.range.7d' });
const windowLabel = computed(() => $t(window.value.labelKey));

const filters = reactive({
  action: undefined as string | undefined,
  actorType: undefined as string | undefined,
  q: '',
});

function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString();
}
function parseTs(v?: string): Date | undefined {
  if (!v) return undefined;
  const t = Date.parse(v);
  return Number.isNaN(t) || t <= 0 ? undefined : new Date(t);
}
function fmtDate(v?: string): string {
  return parseTs(v)?.toLocaleDateString() ?? '—';
}
function fmtTime(v?: string): string {
  return (
    parseTs(v)?.toLocaleTimeString(undefined, {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }) ?? '—'
  );
}
function fmtDateTime(v?: string): string {
  const d = parseTs(v);
  return d ? `${fmtDate(v)} ${fmtTime(v)}` : '—';
}
function pct(part: number, all: number): string {
  return all ? `${((part / all) * 100).toFixed(1)}%` : '0%';
}
function statusTone(s?: number): 'amber' | 'green' | 'red' {
  if (!s) return 'amber';
  if (s < 400) return 'green';
  return s >= 500 ? 'red' : 'amber';
}

function refreshWindow(): boolean {
  if (preset.value === 'custom') {
    if (!customRange.value) {
      message.info($t('logs.audit.messages.selectRange'));
      return false;
    }
    const span = customRange.value[1].diff(customRange.value[0], 'hour', true);
    if (span > 24 * 31) {
      message.warning($t('logs.audit.messages.windowMax'));
      return false;
    }
    window.value = {
      from: customRange.value[0].toISOString(),
      to: customRange.value[1].toISOString(),
      labelKey: 'logs.common.customRange',
    };
    return true;
  }
  const r = RANGE_HOURS.find((x) => x.key === preset.value) ?? RANGE_HOURS[1];
  if (!r) return false;
  window.value = {
    from: new Date(Date.now() - r.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
    labelKey: `logs.common.range.${r.key}`,
  };
  return true;
}

// ── KPI（时间窗级，不随表格过滤变化）──
const stats = ref<{
  bot: number;
  human: number;
  system: number;
  total: number;
}>();
const statsFailed = ref(false);

async function loadStats() {
  statsFailed.value = false;
  try {
    const base: AuditQuery = { from: window.value.from, to: window.value.to };
    const [all, human, bot, system] = await Promise.all([
      listAudits({ ...base, pageSize: 1 }),
      listAudits({ ...base, actorType: 'human', pageSize: 1 }),
      listAudits({ ...base, actorType: 'bot', pageSize: 1 }),
      listAudits({ ...base, actorType: 'system', pageSize: 1 }),
    ]);
    stats.value = {
      total: all.total ?? all.items.length,
      human: human.total ?? human.items.length,
      bot: bot.total ?? bot.items.length,
      system: system.total ?? system.items.length,
    };
  } catch {
    stats.value = undefined;
    statsFailed.value = true;
  }
}

// ── 列表（游标分页）──
const rows = ref<AuditEntry[]>([]);
const nextCursor = ref('');
const total = ref(0);
const loading = ref(false);
const loadingMore = ref(false);

function queryBody(cursor?: string, size = 50): AuditQuery {
  const q: AuditQuery = {
    from: window.value.from,
    to: window.value.to,
    cursor,
    pageSize: size,
  };
  if (filters.actorType) q.actorType = filters.actorType;
  if (filters.action) q.action = filters.action;
  if (filters.q.trim()) q.q = filters.q.trim();
  return q;
}

async function load(cursor?: string) {
  const more = Boolean(cursor);
  if (more) loadingMore.value = true;
  else loading.value = true;
  try {
    const p = await listAudits(queryBody(cursor));
    rows.value = more ? [...rows.value, ...(p.items ?? [])] : (p.items ?? []);
    nextCursor.value = p.nextCursor ?? '';
    total.value = p.total ?? rows.value.length;
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.audit.messages.queryFail'),
    );
    if (!more) {
      rows.value = [];
      total.value = 0;
      nextCursor.value = '';
    }
  } finally {
    loading.value = false;
    loadingMore.value = false;
  }
}

function query() {
  if (!refreshWindow()) return;
  void load();
  void loadStats();
}

function onPreset(key: PresetKey) {
  preset.value = key;
  if (key !== 'custom') {
    customRange.value = undefined;
    query();
  }
}

function onRangeChange(v: unknown) {
  if (!v) {
    if (preset.value === 'custom') preset.value = '7d';
    return;
  }
  preset.value = 'custom';
  customRange.value = v as [Dayjs, Dayjs];
  query();
}

function resetFilters() {
  filters.actorType = undefined;
  filters.action = undefined;
  filters.q = '';
  preset.value = '7d';
  customRange.value = undefined;
  query();
}

// ── 自动刷新 ──
const autoRefresh = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

function watchAutoRefresh(on: boolean) {
  clearInterval(timer);
  timer = undefined;
  if (on) {
    timer = setInterval(() => {
      if (!loading.value && !loadingMore.value) void load();
    }, 60_000);
  }
}

// ── CSV 导出（跟随当前过滤，最多 5000 条）──
const exporting = ref(false);
const verifying = ref(false);
const verified = ref<null | {
  checked: number;
  message: string;
  valid: boolean;
}>(null);

function csvCell(v: unknown): string {
  const s = v === undefined || v === null ? '' : String(v);
  return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
}

async function exportCsv() {
  if (loading.value) return;
  exporting.value = true;
  try {
    const all: AuditEntry[] = [];
    let cursor: string | undefined;
    for (let i = 0; i < 10; i++) {
      const p = await listAudits(queryBody(cursor, 500));
      all.push(...(p.items ?? []));
      cursor = p.nextCursor || undefined;
      if (!cursor || (p.items ?? []).length === 0) break;
    }
    if (all.length === 0) {
      message.warning($t('logs.audit.messages.exportEmpty'));
      return;
    }
    const header = [
      $t('logs.audit.csvHeader.time'),
      $t('logs.audit.csvHeader.actor'),
      $t('logs.audit.csvHeader.type'),
      $t('logs.audit.csvHeader.action'),
      $t('logs.audit.csvHeader.method'),
      $t('logs.audit.csvHeader.path'),
      $t('logs.audit.csvHeader.resource'),
      $t('logs.audit.csvHeader.status'),
      $t('logs.audit.csvHeader.detail'),
    ];
    const lines = [header.map((c) => csvCell(c)).join(',')];
    for (const r of all) {
      lines.push(
        [
          r.ts,
          r.actor ?? '',
          r.actorType,
          r.action,
          r.method,
          r.path,
          r.resource,
          r.status,
          r.detail ?? '',
        ]
          .map((c) => csvCell(c))
          .join(','),
      );
    }
    const blob = new Blob([`\uFEFF${lines.join('\n')}`], {
      type: 'text/csv;charset=utf-8',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `audit-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
    message.success($t('logs.audit.messages.exported', { n: all.length }));
    if (all.length >= 5000) message.info($t('logs.audit.messages.exportLimit'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.audit.messages.exportFail'),
    );
  } finally {
    exporting.value = false;
  }
}

async function verifyChain() {
  verifying.value = true;
  try {
    const res = await verifyAudits(
      window.value.from,
      window.value.to || undefined,
    );
    verified.value = {
      checked: res.checked,
      valid: res.valid,
      message:
        res.message ??
        (res.valid
          ? $t('logs.audit.messages.chainOk')
          : $t('logs.audit.messages.chainBad')),
    };
    if (res.valid) {
      message.success($t('logs.audit.messages.chainValid', { n: res.checked }));
    } else {
      message.error(
        $t('logs.audit.messages.chainInvalid', {
          message: res.message ?? $t('logs.audit.messages.chainBad'),
          id: res.brokenId ?? '?',
        }),
      );
    }
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('logs.audit.messages.verifyFail'),
    );
  } finally {
    verifying.value = false;
  }
}

// ── Detail drawer ──
const detail = ref<AuditEntry>();
const [DetailDrawer, detailDrawerApi] = useVbenDrawer({
  class: 'w-[620px]',
  footer: false,
  title: $t('logs.audit.detail.title'),
});

function openDetail(r: AuditEntry) {
  detail.value = r;
  detailDrawerApi.open();
}

function rowProps(record: AuditEntry) {
  return { class: 'ipam-row-link', onClick: () => openDetail(record) };
}

const prettyDetail = computed(() => {
  const raw = detail.value?.detail;
  if (!raw) return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
});

const prettyPayload = computed(() => {
  const raw = detail.value?.payload;
  if (!raw) return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
});

const prettyBefore = computed(() => {
  const raw = detail.value?.before;
  if (!raw) return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
});

async function copyDetail() {
  if (!detail.value) return;
  try {
    await navigator.clipboard.writeText(JSON.stringify(detail.value, null, 2));
    message.success($t('logs.audit.detail.copied'));
  } catch {
    message.warning($t('logs.common.clipboardDenied'));
  }
}

const columns = computed(() => [
  {
    dataIndex: 'ts',
    key: 'ts',
    title: $t('logs.audit.columns.time'),
    width: 142,
  },
  {
    dataIndex: 'actor',
    key: 'actor',
    title: $t('logs.audit.columns.actor'),
    width: 190,
  },
  {
    dataIndex: 'actorType',
    key: 'actorType',
    title: $t('logs.audit.columns.type'),
    width: 84,
  },
  {
    dataIndex: 'action',
    key: 'action',
    title: $t('logs.audit.columns.action'),
    width: 96,
  },
  {
    dataIndex: 'resource',
    key: 'resource',
    title: $t('logs.audit.columns.resource'),
    ellipsis: true,
  },
  {
    dataIndex: 'status',
    key: 'status',
    title: $t('logs.audit.columns.status'),
    width: 88,
  },
]);

const footerText = computed(() => {
  const base = $t('logs.audit.footer.loaded', { n: fmtInt(rows.value.length) });
  return total.value > rows.value.length
    ? base +
        $t('logs.audit.footer.remaining', {
          n: fmtInt(total.value - rows.value.length),
        })
    : base;
});

onMounted(() => {
  refreshWindow();
  void load();
  void loadStats();
});

onBeforeUnmount(() => {
  clearInterval(timer);
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:shield-check"
      :title="$t('logs.audit.title')"
      :description="$t('logs.audit.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock" class="size-3.5" />
          {{ $t('logs.audit.meta', { range: windowLabel, n: fmtInt(total) }) }}
        </span>
        <Tag
          v-if="verified"
          class="m-0"
          :color="verified.valid ? 'green' : 'red'"
        >
          {{
            verified.valid
              ? $t('logs.audit.chain.valid', { n: verified.checked })
              : $t('logs.audit.chain.invalid')
          }}
        </Tag>
        <Button size="small" :loading="loading" @click="query">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('logs.common.refresh')
          }}
        </Button>
        <Button size="small" :loading="verifying" @click="verifyChain">
          <IconifyIcon icon="lucide:shield-check" class="mr-1 size-3.5" />{{
            $t('logs.audit.chain.verify')
          }}
        </Button>
        <Button
          size="small"
          type="primary"
          :loading="exporting"
          @click="exportCsv"
        >
          <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />{{
            $t('logs.common.exportCsv')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('logs.audit.kpi.total')"
        :value="stats ? fmtInt(stats.total) : statsFailed ? '—' : '…'"
        icon="lucide:activity"
        tone="blue"
        :hint="$t('logs.audit.kpi.totalHint', { range: windowLabel })"
      />
      <StatCard
        :label="$t('logs.audit.kpi.human')"
        :value="stats ? fmtInt(stats.human) : statsFailed ? '—' : '…'"
        icon="lucide:user-round"
        tone="cyan"
        :hint="
          $t('logs.audit.kpi.share', {
            pct: pct(stats?.human ?? 0, stats?.total ?? 0),
          })
        "
      />
      <StatCard
        :label="$t('logs.audit.kpi.bot')"
        :value="stats ? fmtInt(stats.bot) : statsFailed ? '—' : '…'"
        icon="lucide:bot"
        tone="violet"
        :hint="
          $t('logs.audit.kpi.share', {
            pct: pct(stats?.bot ?? 0, stats?.total ?? 0),
          })
        "
      />
      <StatCard
        :label="$t('logs.audit.kpi.system')"
        :value="stats ? fmtInt(stats.system) : statsFailed ? '—' : '…'"
        icon="lucide:server-cog"
        tone="green"
        :hint="
          $t('logs.audit.kpi.share', {
            pct: pct(stats?.system ?? 0, stats?.total ?? 0),
          })
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:clipboard-list" />
          {{ $t('logs.audit.card.title') }}
          <em class="ipam-card-sub">{{
            $t('logs.audit.card.loaded', {
              loaded: fmtInt(rows.length),
              total: fmtInt(total),
            })
          }}</em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <span class="ipam-hint">{{ $t('logs.audit.card.order') }}</span>
          <span class="ipam-field__label">{{
            $t('logs.audit.card.autoRefresh')
          }}</span>
          <Switch
            v-model:checked="autoRefresh"
            size="small"
            @change="watchAutoRefresh(autoRefresh)"
          />
        </div>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-seg">
          <button
            v-for="r in ranges"
            :key="r.key"
            type="button"
            :data-active="preset === r.key"
            @click="onPreset(r.key)"
          >
            {{ r.label }}
          </button>
          <button
            type="button"
            :data-active="preset === 'custom'"
            @click="onPreset('custom')"
          >
            {{ $t('logs.common.custom') }}
          </button>
        </div>
        <DatePicker.RangePicker
          :value="customRange"
          :allow-clear="true"
          :show-time="{ format: 'HH:mm:ss' }"
          format="YYYY-MM-DD HH:mm:ss"
          style="width: 340px"
          :placeholder="[
            $t('logs.common.startTime'),
            $t('logs.common.endTime'),
          ]"
          @change="onRangeChange"
        />
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="resetFilters">
          {{ $t('logs.common.reset') }}
        </Button>
        <Button type="primary" size="small" @click="query">
          <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />{{
            $t('logs.common.query')
          }}
        </Button>
      </div>

      <div class="ipam-filter-grid">
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.audit.filters.actorType')
          }}</span>
          <Select
            v-model:value="filters.actorType"
            allow-clear
            :placeholder="$t('logs.audit.filters.actorPlaceholder')"
            :options="[
              { value: 'human', label: $t('logs.audit.filters.human') },
              { value: 'bot', label: $t('logs.audit.filters.bot') },
              { value: 'system', label: $t('logs.audit.filters.system') },
            ]"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">{{
            $t('logs.audit.filters.action')
          }}</span>
          <Select
            v-model:value="filters.action"
            allow-clear
            show-search
            option-filter-prop="label"
            :placeholder="$t('logs.audit.filters.actionPlaceholder')"
            :options="ACTION_OPTIONS"
          />
        </label>
        <label class="ipam-filter-field ipam-filter-field--wide">
          <span class="ipam-filter-field__label">{{
            $t('logs.audit.filters.resource')
          }}</span>
          <Input
            v-model:value="filters.q"
            allow-clear
            :placeholder="$t('logs.audit.filters.resourcePlaceholder')"
            @press-enter="query"
          />
        </label>
      </div>

      <Table
        class="mt-3"
        :columns="columns"
        :custom-row="rowProps"
        :data-source="rows"
        :loading="loading"
        :locale="{ emptyText: $t('logs.audit.empty') }"
        :pagination="false"
        :row-key="(r: AuditEntry) => String(r.id)"
        :scroll="{ x: 880 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'ts'">
            <span class="ipam-log-time">
              <b>{{ fmtTime(record.ts) }}</b>
              <span>{{ fmtDate(record.ts) }}</span>
            </span>
          </template>
          <template v-else-if="column.key === 'actor'">
            <span class="ipam-cell-stack">
              <span class="ipam-cell-title">{{
                record.actor || 'anonymous'
              }}</span>
              <span
                v-if="record.tokenSub"
                class="ipam-cell-meta"
                :title="record.tokenSub"
              >
                {{ record.tokenSub }}
              </span>
            </span>
          </template>
          <template v-else-if="column.key === 'actorType'">
            <Tag class="m-0" :color="ACTOR_META[record.actorType]?.color">
              {{ ACTOR_META[record.actorType]?.label ?? record.actorType }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <Tag class="m-0" :color="ACTION_COLOR[record.action] ?? 'default'">
              {{ record.action }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'resource'">
            <span class="ipam-cell-stack">
              <span class="ipam-cell-title" :title="record.resource">{{
                record.resource
              }}</span>
              <span
                class="ipam-cell-meta ipam-mono"
                :title="`${record.method} ${record.path}`"
              >
                {{ record.method }} {{ record.path }}
              </span>
            </span>
          </template>
          <template v-else-if="column.key === 'status'">
            <Tag class="m-0" :color="statusTone(record.status)">
              {{ record.status }}
            </Tag>
          </template>
        </template>
      </Table>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ footerText }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button
          v-if="nextCursor"
          size="small"
          :loading="loadingMore"
          @click="load(nextCursor)"
        >
          {{ $t('logs.audit.footer.loadMore') }}
        </Button>
        <span v-else-if="rows.length > 0" class="ipam-hint">{{
          $t('logs.audit.footer.lastPage')
        }}</span>
      </div>
    </Card>

    <DetailDrawer>
      <template v-if="detail">
        <div class="ipam-detail-head">
          <Tag class="m-0" :color="ACTOR_META[detail.actorType]?.color">
            {{ ACTOR_META[detail.actorType]?.label ?? detail.actorType }}
          </Tag>
          <Tag class="m-0" :color="ACTION_COLOR[detail.action] ?? 'default'">
            {{ detail.action }}
          </Tag>
          <Tag class="m-0" :color="statusTone(detail.status)">
            {{ detail.status }}
          </Tag>
          <span class="ipam-toolbar__spacer"></span>
          <Button size="small" @click="copyDetail">
            <IconifyIcon icon="lucide:copy" class="mr-1 size-3.5" />{{
              $t('logs.common.copyJson')
            }}
          </Button>
        </div>

        <div class="ipam-detail-grid">
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.time') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ fmtDateTime(detail.ts) }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.actor') }}
            </div>
            <div class="ipam-detail-item__value">
              {{ detail.actor || 'anonymous' }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.tokenSub') }}
            </div>
            <div class="ipam-detail-item__value ipam-mono">
              {{ detail.tokenSub || '—' }}
            </div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.methodStatus') }}
            </div>
            <div class="ipam-detail-item__value ipam-mono">
              {{ detail.method }} · {{ detail.status }}
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.path') }}
            </div>
            <div class="ipam-detail-item__value ipam-mono">
              {{ detail.path }}
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.resource') }}
            </div>
            <div class="ipam-detail-item__value">{{ detail.resource }}</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.extra') }}
            </div>
            <pre v-if="prettyDetail" class="ipam-code-block mt-1">{{
              prettyDetail
            }}</pre>
            <div v-else class="ipam-detail-item__value">—</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.before') }}
            </div>
            <pre v-if="prettyBefore" class="ipam-code-block mt-1">{{
              prettyBefore
            }}</pre>
            <div v-else class="ipam-detail-item__value">
              {{ $t('logs.audit.detail.beforeEmpty') }}
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.payload') }}
            </div>
            <pre v-if="prettyPayload" class="ipam-code-block mt-1">{{
              prettyPayload
            }}</pre>
            <div v-else class="ipam-detail-item__value">—</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">
              {{ $t('logs.audit.detail.chainHash') }}
            </div>
            <div class="ipam-detail-item__value ipam-mono break-all">
              {{ detail.chainHash || $t('logs.audit.detail.chainHashEmpty') }}
            </div>
          </div>
        </div>
      </template>
    </DetailDrawer>
  </div>
</template>
