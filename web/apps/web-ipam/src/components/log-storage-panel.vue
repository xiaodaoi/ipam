<script setup lang="ts">
import type { LogArchive, LogSettings, LogStorageStats } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  deleteLogArchive,
  downloadLogArchive,
  exportLogArchive,
  getLogSettings,
  getLogStorageStats,
  listLogArchives,
  updateLogSettings,
} from '#/api/ipam';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

withDefaults(defineProps<{ kpi?: boolean }>(), { kpi: false });

const logForm = reactive<LogSettings>({
  retentionDays: 180,
  archiveKeepMonths: 12,
  autoExport: true,
});
const logSaving = ref(false);
const stats = ref<LogStorageStats>();
const archives = ref<LogArchive[]>([]);
const statsLoading = ref(false);
const archiveLoading = ref(false);
const exportMonthInput = ref('');
const keyword = ref('');
const busyMonth = ref('');
const exporting = ref(false);

function fmtBytes(n?: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
function fmtInt(n?: number): string {
  return (n ?? 0).toLocaleString();
}
function fmtDate(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString(undefined, { hour12: false });
}

const monthValid = computed(() => /^\d{4}-(0[1-9]|1[0-2])$/.test(exportMonthInput.value.trim()));
const partitions = computed(() =>
  [...(stats.value?.partitions ?? [])].toSorted((a, b) => b.month.localeCompare(a.month)),
);
const partitionMax = computed(() =>
  Math.max(1, ...partitions.value.map((p) => p.rowCount ?? 0)),
);
const archiveRows = computed(() =>
  [...archives.value]
    .filter((a) => !keyword.value.trim() || a.month.includes(keyword.value.trim()))
    .toSorted((a, b) => b.month.localeCompare(a.month)),
);
const latestArchive = computed(() => archiveRows.value[0]?.month ?? '');
const archiveColumns = computed(() => [
  { dataIndex: 'month', key: 'month', title: $t('logs.storage.archive.col.month'), width: 116 },
  {
    dataIndex: 'rowCount',
    key: 'rowCount',
    title: $t('logs.storage.archive.col.rows'),
    sorter: (a: LogArchive, b: LogArchive) => (a.rowCount ?? 0) - (b.rowCount ?? 0),
    width: 120,
  },
  {
    dataIndex: 'fileSize',
    key: 'fileSize',
    title: $t('logs.storage.archive.col.size'),
    sorter: (a: LogArchive, b: LogArchive) => (a.fileSize ?? 0) - (b.fileSize ?? 0),
    width: 120,
  },
  { dataIndex: 'exportedBy', key: 'exportedBy', title: $t('logs.storage.archive.col.source'), width: 90 },
  {
    dataIndex: 'createdAt',
    key: 'createdAt',
    title: $t('logs.storage.archive.col.created'),
    sorter: (a: LogArchive, b: LogArchive) => Date.parse(a.createdAt) - Date.parse(b.createdAt),
  },
  { dataIndex: 'actions', key: 'actions', title: $t('logs.storage.archive.col.actions'), width: 130 },
]);

async function loadSettings() {
  try {
    const s = await getLogSettings();
    logForm.retentionDays = s.retentionDays;
    logForm.archiveKeepMonths = s.archiveKeepMonths;
    logForm.autoExport = s.autoExport;
  } catch {
    /* 策略读取失败不阻塞页面，保留默认值 */
  }
}

async function loadStats() {
  statsLoading.value = true;
  try {
    stats.value = await getLogStorageStats();
  } catch {
    stats.value = undefined;
  } finally {
    statsLoading.value = false;
  }
}

async function loadArchives() {
  archiveLoading.value = true;
  try {
    const data = await listLogArchives();
    archives.value = data.items ?? [];
  } catch {
    archives.value = [];
  } finally {
    archiveLoading.value = false;
  }
}

async function saveSettings() {
  if (logForm.retentionDays < 1 || logForm.retentionDays > 3650) {
    message.warning($t('logs.storage.policy.retentionWarn'));
    return;
  }
  if (logForm.archiveKeepMonths < 1 || logForm.archiveKeepMonths > 120) {
    message.warning($t('logs.storage.policy.monthsWarn'));
    return;
  }
  logSaving.value = true;
  try {
    await updateLogSettings({
      retentionDays: logForm.retentionDays,
      archiveKeepMonths: logForm.archiveKeepMonths,
      autoExport: logForm.autoExport,
    });
    message.success($t('logs.storage.policy.saved'));
    await loadStats();
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('logs.storage.policy.saveFail'));
  } finally {
    logSaving.value = false;
  }
}

async function doExport() {
  const m = exportMonthInput.value.trim();
  if (!monthValid.value) {
    message.warning($t('logs.storage.archive.monthWarn'));
    return;
  }
  exporting.value = true;
  try {
    await exportLogArchive(m);
    message.success($t('logs.storage.archive.exported', { m }));
    await Promise.all([loadArchives(), loadStats()]);
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('logs.storage.archive.exportFail'));
  } finally {
    exporting.value = false;
  }
}

function pickMonth(m: string) {
  exportMonthInput.value = m;
}

async function removeArchive(month: string) {
  busyMonth.value = month;
  try {
    await deleteLogArchive(month);
    message.success($t('logs.storage.archive.deleted', { m: month }));
    await Promise.all([loadArchives(), loadStats()]);
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('logs.storage.archive.deleteFail'));
  } finally {
    busyMonth.value = '';
  }
}

async function downloadArchive(month: string) {
  busyMonth.value = month;
  try {
    await downloadLogArchive(month);
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('logs.storage.archive.downloadFail'));
  } finally {
    busyMonth.value = '';
  }
}

function reloadAll() {
  void loadSettings();
  void loadStats();
  void loadArchives();
}

onMounted(reloadAll);
</script>

<template>
  <div class="ipam-stack">
    <div v-if="kpi" class="ipam-stat-grid">
      <StatCard
        :label="$t('logs.storage.kpi.rows')"
        :value="statsLoading ? '…' : fmtInt(stats?.totalRows)"
        icon="lucide:rows-3"
        tone="blue"
        :hint="
          stats?.earliestMonth
            ? $t('logs.storage.kpi.rowsHint', { m: stats.earliestMonth })
            : $t('logs.storage.kpi.rowsEmpty')
        "
      />
      <StatCard
        :label="$t('logs.storage.kpi.disk')"
        :value="fmtBytes(stats?.diskBytes)"
        icon="lucide:hard-drive"
        tone="cyan"
        :hint="$t('logs.storage.kpi.diskHint', { n: partitions.length })"
      />
      <StatCard
        :label="$t('logs.storage.kpi.archives')"
        :value="fmtInt(archives.length)"
        :unit="$t('logs.storage.kpi.unit')"
        icon="lucide:archive"
        tone="violet"
        :hint="
          latestArchive
            ? $t('logs.storage.kpi.archiveHint', { m: latestArchive })
            : $t('logs.storage.kpi.archiveEmpty')
        "
      />
      <StatCard
        :label="$t('logs.storage.kpi.ttl')"
        :value="stats?.retentionDays ?? logForm.retentionDays"
        :unit="$t('logs.storage.kpi.ttlUnit')"
        icon="lucide:timer-reset"
        tone="green"
        :hint="
          $t('logs.storage.kpi.ttlHint', { n: logForm.archiveKeepMonths })
        "
      />
    </div>

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:timer-reset" />
            {{ $t('logs.storage.policy.title') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">ClickHouse TTL</span>
        </template>

        <div class="ipam-form-grid">
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{ $t('logs.storage.policy.retention') }}</span>
            <div class="flex items-center gap-2">
              <InputNumber
                v-model:value="logForm.retentionDays"
                :min="1"
                :max="3650"
                class="flex-1"
              />
              <span class="ipam-hint">{{ $t('logs.storage.policy.days') }}</span>
            </div>
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{ $t('logs.storage.policy.keepMonths') }}</span>
            <div class="flex items-center gap-2">
              <InputNumber
                v-model:value="logForm.archiveKeepMonths"
                :min="1"
                :max="120"
                class="flex-1"
              />
              <span class="ipam-hint">{{ $t('logs.storage.policy.months') }}</span>
            </div>
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">{{ $t('logs.storage.policy.auto') }}</span>
            <div class="flex h-8 items-center gap-2">
              <Switch v-model:checked="logForm.autoExport" size="small" />
              <span class="ipam-hint">
                {{
                  logForm.autoExport
                    ? $t('logs.storage.policy.autoOn')
                    : $t('logs.storage.policy.autoOff')
                }}
              </span>
            </div>
          </div>
          <div class="ipam-form-hint ipam-form-field--wide">
            {{ $t('logs.storage.policy.hint') }}
          </div>
          <div class="ipam-form-field--wide flex justify-end">
            <Button type="primary" :loading="logSaving" @click="saveSettings">
              <IconifyIcon icon="lucide:save" class="mr-1" />{{ $t('logs.storage.policy.save') }}
            </Button>
          </div>
        </div>
      </Card>

      <Card class="lg:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:database" />
            {{ $t('logs.storage.online.title') }}
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">{{ $t('logs.storage.online.hint') }}</span>
        </template>

        <div class="ipam-metric-row">
          <div class="ipam-metric">
            <div class="ipam-metric__label">{{ $t('logs.storage.online.totalRows') }}</div>
            <div class="ipam-metric__value">{{ fmtInt(stats?.totalRows) }}</div>
          </div>
          <div class="ipam-metric">
            <div class="ipam-metric__label">{{ $t('logs.storage.online.disk') }}</div>
            <div class="ipam-metric__value">{{ fmtBytes(stats?.diskBytes) }}</div>
          </div>
          <div class="ipam-metric">
            <div class="ipam-metric__label">{{ $t('logs.storage.online.earliest') }}</div>
            <div class="ipam-metric__value">{{ stats?.earliestMonth || '—' }}</div>
          </div>
          <div class="ipam-metric">
            <div class="ipam-metric__label">{{ $t('logs.storage.online.ttl') }}</div>
            <div class="ipam-metric__value">
              {{ stats?.retentionDays ?? logForm.retentionDays }}<small class="ml-1 text-xs font-normal">{{ $t('logs.storage.kpi.ttlUnit') }}</small>
            </div>
          </div>
        </div>

        <div v-if="partitions.length > 0" class="mt-4">
          <div class="mb-2 flex items-center justify-between text-xs text-gray-400">
            <span>{{ $t('logs.storage.online.partitionHint') }}</span>
            <span>{{ $t('logs.storage.online.partitions', { n: partitions.length }) }}</span>
          </div>
          <div class="ipam-partition-list">
            <button
              v-for="p in partitions"
              :key="p.month"
              type="button"
              class="ipam-partition"
              :class="{ 'ipam-partition--active': exportMonthInput === p.month }"
              :title="$t('logs.storage.online.pick', { m: p.month })"
              @click="pickMonth(p.month)"
            >
              <span class="ipam-partition__month ipam-mono">{{ p.month }}</span>
              <span class="ipam-partition__meta">
                {{
                  $t('logs.storage.online.rows', {
                    n: fmtInt(p.rowCount),
                    size: fmtBytes(p.diskBytes),
                  })
                }}
              </span>
              <span class="ipam-progress ipam-partition__bar">
                <i :style="{ width: `${Math.max(4, ((p.rowCount ?? 0) / partitionMax) * 100)}%` }"></i>
              </span>
            </button>
          </div>
        </div>
        <div v-else class="ipam-empty ipam-empty--compact mt-3">
          <div class="ipam-empty__title">{{ $t('logs.storage.online.emptyTitle') }}</div>
          <div class="ipam-empty__desc">{{ $t('logs.storage.online.emptyDesc') }}</div>
        </div>
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:archive" />
          {{ $t('logs.storage.archive.title') }}
          <em class="ipam-card-sub">
            {{ $t('logs.storage.archive.count', { n: archives.length }) }}
          </em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <Input
            v-model:value="keyword"
            allow-clear
            :placeholder="$t('logs.storage.archive.filter')"
            style="width: 130px"
            size="small"
          />
          <Button size="small" :loading="archiveLoading" @click="loadArchives">
            <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{ $t('logs.storage.archive.refresh') }}
          </Button>
        </div>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('logs.storage.archive.exportMonth') }}</span>
          <Input
            v-model:value="exportMonthInput"
            placeholder="YYYY-MM"
            style="width: 140px"
            @press-enter="doExport"
          />
        </div>
        <Button
          type="primary"
          size="small"
          :disabled="!monthValid"
          :loading="exporting"
          @click="doExport"
        >
          <IconifyIcon icon="lucide:package-plus" class="mr-1 size-3.5" />{{ $t('logs.storage.archive.export') }}
        </Button>
        <span class="ipam-toolbar__spacer"></span>
        <span class="ipam-hint">{{ $t('logs.storage.archive.hint') }}</span>
      </div>

      <Table
        :columns="archiveColumns"
        :data-source="archiveRows"
        :loading="archiveLoading"
        :locale="{
          emptyText:
            archives.length === 0
              ? $t('logs.storage.archive.emptyNoArchive')
              : $t('logs.storage.archive.emptyNoMatch'),
        }"
        :pagination="{
          pageSize: 8,
          position: ['bottomLeft'],
          showTotal: (t: number) => $t('logs.storage.archive.total', { n: t }),
        }"
        :row-key="(a: LogArchive) => a.month"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'month'">
            <span class="ipam-cell-stack">
              <span class="ipam-mono text-xs font-semibold">{{ record.month }}</span>
              <span v-if="record.fileHash" class="ipam-cell-meta ipam-mono" :title="record.fileHash">
                {{ record.fileHash.slice(0, 16) }}…
              </span>
            </span>
          </template>
          <template v-else-if="column.key === 'rowCount'">{{ fmtInt(record.rowCount) }}</template>
          <template v-else-if="column.key === 'fileSize'">{{ fmtBytes(record.fileSize) }}</template>
          <template v-else-if="column.key === 'exportedBy'">
            <Tag class="m-0" :color="record.exportedBy === 'system' ? 'default' : 'blue'">
              {{
                record.exportedBy === 'system'
                  ? $t('logs.storage.archive.auto')
                  : $t('logs.storage.archive.manual')
              }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'createdAt'">{{ fmtDate(record.createdAt) }}</template>
          <template v-else-if="column.key === 'actions'">
            <Button
              size="small"
              :loading="busyMonth === record.month"
              @click="downloadArchive(record.month)"
            >
              {{ $t('logs.storage.archive.download') }}
            </Button>
            <Popconfirm
              :title="$t('logs.storage.archive.deleteTitle')"
              :description="$t('logs.storage.archive.deleteDesc')"
              :ok-text="$t('logs.storage.archive.delete')"
              :cancel-text="$t('logs.storage.archive.cancel')"
              @confirm="removeArchive(record.month)"
            >
              <Button size="small" danger class="ml-2">
                {{ $t('logs.storage.archive.delete') }}
              </Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>
