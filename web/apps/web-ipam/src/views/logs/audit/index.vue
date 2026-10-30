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

const RANGES = [
  { hours: 24, key: '24h', label: '近 24 小时' },
  { hours: 168, key: '7d', label: '近 7 天' },
  { hours: 720, key: '30d', label: '近 30 天' },
] as const;
type PresetKey = 'custom' | (typeof RANGES)[number]['key'];

const ACTION_OPTIONS = [
  { label: 'create（新建）', value: 'create' },
  { label: 'update（更新）', value: 'update' },
  { label: 'delete（删除）', value: 'delete' },
  { label: 'login（登录）', value: 'login' },
  { label: 'logout（登出）', value: 'logout' },
];
const ACTOR_META: Record<string, { color: string; label: string }> = {
  human: { color: 'blue', label: '人工' },
  bot: { color: 'purple', label: 'Bot' },
  system: { color: 'default', label: '系统' },
};
const ACTION_COLOR: Record<string, string> = {
  create: 'green',
  delete: 'red',
  update: 'blue',
  login: 'cyan',
  logout: 'default',
};

const preset = ref<PresetKey>('7d');
const customRange = ref<[Dayjs, Dayjs]>();
const window = ref({ from: '', to: '', label: '' });

const filters = reactive({
  action: undefined as string | undefined,
  actorType: undefined as string | undefined,
  q: '',
});

function fmtInt(v?: number): string {
  return (v ?? 0).toLocaleString('zh-CN');
}
function parseTs(v?: string): Date | undefined {
  if (!v) return undefined;
  const t = Date.parse(v);
  return Number.isNaN(t) || t <= 0 ? undefined : new Date(t);
}
function fmtDate(v?: string): string {
  return parseTs(v)?.toLocaleDateString('zh-CN') ?? '—';
}
function fmtTime(v?: string): string {
  return (
    parseTs(v)?.toLocaleTimeString('zh-CN', {
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
      message.info('请选择自定义时间范围');
      return false;
    }
    const span = customRange.value[1].diff(customRange.value[0], 'hour', true);
    if (span > 24 * 31) {
      message.warning('审计查询时间窗最大 31 天');
      return false;
    }
    window.value = {
      from: customRange.value[0].toISOString(),
      to: customRange.value[1].toISOString(),
      label: '自定义区间',
    };
    return true;
  }
  const r = RANGES.find((x) => x.key === preset.value) ?? RANGES[1]!;
  window.value = {
    from: new Date(Date.now() - r.hours * 3_600_000).toISOString(),
    to: new Date().toISOString(),
    label: r.label,
  };
  return true;
}

// ── KPI（时间窗级，不随表格过滤变化）──
const stats = ref<{ bot: number; human: number; system: number; total: number }>();
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
    message.error(error instanceof Error ? error.message : '审计查询失败');
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
const verified = ref<null | { checked: number; message: string; valid: boolean }>(null);

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
      message.warning('当前条件下没有可导出的审计记录');
      return;
    }
    const header = ['时间', '调用者', '类型', '动作', '方法', '路径', '资源', '状态码', '详情'];
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
    message.success(`已导出 ${all.length} 条审计记录`);
    if (all.length >= 5000) message.info('单次最多导出 5000 条，请缩小时间窗');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导出失败');
  } finally {
    exporting.value = false;
  }
}

async function verifyChain() {
  verifying.value = true;
  try {
    const res = await verifyAudits(window.value.from, window.value.to || undefined);
    verified.value = {
      checked: res.checked,
      valid: res.valid,
      message: res.message ?? (res.valid ? '审计链完整' : '审计链异常'),
    };
    if (res.valid) {
      message.success(`审计链完整（校验 ${res.checked} 条）`);
    } else {
      message.error(`${res.message ?? '审计链异常'}（ID ${res.brokenId ?? '?'}）`);
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '完整性校验失败');
  } finally {
    verifying.value = false;
  }
}

// ── 详情抽屉 ──
const detail = ref<AuditEntry>();
const [DetailDrawer, detailDrawerApi] = useVbenDrawer({
  class: 'w-[620px]',
  footer: false,
  title: '审计详情',
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

async function copyDetail() {
  if (!detail.value) return;
  try {
    await navigator.clipboard.writeText(JSON.stringify(detail.value, null, 2));
    message.success('已复制当前审计 JSON');
  } catch {
    message.warning('浏览器未授予剪贴板权限');
  }
}

const columns = [
  { dataIndex: 'ts', key: 'ts', title: '时间', width: 142 },
  { dataIndex: 'actor', key: 'actor', title: '调用者', width: 190 },
  { dataIndex: 'actorType', key: 'actorType', title: '类型', width: 84 },
  { dataIndex: 'action', key: 'action', title: '动作', width: 96 },
  { dataIndex: 'resource', key: 'resource', title: '资源', ellipsis: true },
  { dataIndex: 'status', key: 'status', title: '状态', width: 88 },
];

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
      title="操作审计"
      description="记录人工、Bot 与系统对配置资源的变更操作，支持调用者/动作/资源组合检索、明细下钻与 CSV 归档。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock" class="size-3.5" />
          {{ window.label }} · 共 {{ fmtInt(total) }} 条
        </span>
        <Tag v-if="verified" class="m-0" :color="verified.valid ? 'green' : 'red'">
          {{ verified.valid ? `链完整 ${verified.checked}` : '链异常' }}
        </Tag>
        <Button size="small" :loading="loading" @click="query">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button size="small" :loading="verifying" @click="verifyChain">
          <IconifyIcon icon="lucide:shield-check" class="mr-1 size-3.5" />完整性校验
        </Button>
        <Button size="small" type="primary" :loading="exporting" @click="exportCsv">
          <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />导出 CSV
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="操作总数"
        :value="stats ? fmtInt(stats.total) : statsFailed ? '—' : '…'"
        icon="lucide:activity"
        tone="blue"
        :hint="`${window.label} · 仅统计变更类请求`"
      />
      <StatCard
        label="人工操作"
        :value="stats ? fmtInt(stats.human) : statsFailed ? '—' : '…'"
        icon="lucide:user-round"
        tone="cyan"
        :hint="`占操作总数 ${pct(stats?.human ?? 0, stats?.total ?? 0)}`"
      />
      <StatCard
        label="Bot 调用"
        :value="stats ? fmtInt(stats.bot) : statsFailed ? '—' : '…'"
        icon="lucide:bot"
        tone="violet"
        :hint="`占操作总数 ${pct(stats?.bot ?? 0, stats?.total ?? 0)}`"
      />
      <StatCard
        label="系统自动"
        :value="stats ? fmtInt(stats.system) : statsFailed ? '—' : '…'"
        icon="lucide:server-cog"
        tone="green"
        :hint="`占操作总数 ${pct(stats?.system ?? 0, stats?.total ?? 0)}`"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:clipboard-list" />
          审计事件
          <em class="ipam-card-sub">已加载 {{ fmtInt(rows.length) }} / {{ fmtInt(total) }}</em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <span class="ipam-hint">按时间倒序 · 游标分页</span>
          <span class="ipam-field__label">自动刷新</span>
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
            v-for="r in RANGES"
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
            自定义
          </button>
        </div>
        <DatePicker.RangePicker
          :value="customRange"
          :allow-clear="true"
          :show-time="{ format: 'HH:mm:ss' }"
          format="YYYY-MM-DD HH:mm:ss"
          style="width: 340px"
          :placeholder="['开始时间', '结束时间']"
          @change="onRangeChange"
        />
        <div class="ipam-toolbar__spacer"></div>
        <Button size="small" @click="resetFilters">重置</Button>
        <Button type="primary" size="small" @click="query">
          <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />查询
        </Button>
      </div>

      <div class="ipam-filter-grid">
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">调用者类型</span>
          <Select
            v-model:value="filters.actorType"
            allow-clear
            placeholder="全部调用者"
            :options="[
              { value: 'human', label: '人工' },
              { value: 'bot', label: 'Bot' },
              { value: 'system', label: '系统' },
            ]"
          />
        </label>
        <label class="ipam-filter-field">
          <span class="ipam-filter-field__label">动作</span>
          <Select
            v-model:value="filters.action"
            allow-clear
            show-search
            option-filter-prop="label"
            placeholder="全部动作"
            :options="ACTION_OPTIONS"
          />
        </label>
        <label class="ipam-filter-field ipam-filter-field--wide">
          <span class="ipam-filter-field__label">资源 / 路径</span>
          <Input
            v-model:value="filters.q"
            allow-clear
            placeholder="按资源名或请求路径子串匹配，如 /subnets"
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
        :locale="{ emptyText: '当前时间窗与过滤条件下没有审计记录' }"
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
              <span class="ipam-cell-title">{{ record.actor || 'anonymous' }}</span>
              <span v-if="record.tokenSub" class="ipam-cell-meta" :title="record.tokenSub">
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
              <span class="ipam-cell-title" :title="record.resource">{{ record.resource }}</span>
              <span class="ipam-cell-meta ipam-mono" :title="`${record.method} ${record.path}`">
                {{ record.method }} {{ record.path }}
              </span>
            </span>
          </template>
          <template v-else-if="column.key === 'status'">
            <Tag class="m-0" :color="statusTone(record.status)">{{ record.status }}</Tag>
          </template>
        </template>
      </Table>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">
          已加载 {{ fmtInt(rows.length) }} 条{{ total > rows.length ? `，尚有 ${fmtInt(total - rows.length)} 条` : '' }}
        </span>
        <span class="ipam-toolbar__spacer"></span>
        <Button
          v-if="nextCursor"
          size="small"
          :loading="loadingMore"
          @click="load(nextCursor)"
        >
          加载更多
        </Button>
        <span v-else-if="rows.length > 0" class="ipam-hint">已到最后一页</span>
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
          <Tag class="m-0" :color="statusTone(detail.status)">{{ detail.status }}</Tag>
          <span class="ipam-toolbar__spacer"></span>
          <Button size="small" @click="copyDetail">
            <IconifyIcon icon="lucide:copy" class="mr-1 size-3.5" />复制 JSON
          </Button>
        </div>

        <div class="ipam-detail-grid">
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">时间</div>
            <div class="ipam-detail-item__value">{{ fmtDateTime(detail.ts) }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">调用者</div>
            <div class="ipam-detail-item__value">{{ detail.actor || 'anonymous' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">Token 主体</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.tokenSub || '—' }}</div>
          </div>
          <div class="ipam-detail-item">
            <div class="ipam-detail-item__label">请求方法 / 状态</div>
            <div class="ipam-detail-item__value ipam-mono">
              {{ detail.method }} · {{ detail.status }}
            </div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">请求路径</div>
            <div class="ipam-detail-item__value ipam-mono">{{ detail.path }}</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">资源</div>
            <div class="ipam-detail-item__value">{{ detail.resource }}</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">补充说明</div>
            <pre v-if="prettyDetail" class="ipam-code-block mt-1">{{ prettyDetail }}</pre>
            <div v-else class="ipam-detail-item__value">—</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">脱敏请求体</div>
            <pre v-if="prettyPayload" class="ipam-code-block mt-1">{{ prettyPayload }}</pre>
            <div v-else class="ipam-detail-item__value">—</div>
          </div>
          <div class="ipam-detail-item ipam-detail-item--wide">
            <div class="ipam-detail-item__label">完整性链哈希</div>
            <div class="ipam-detail-item__value ipam-mono break-all">
              {{ detail.chainHash || '历史记录未生成链哈希' }}
            </div>
          </div>
        </div>
      </template>
    </DetailDrawer>
  </div>
</template>
