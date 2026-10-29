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
  return (n ?? 0).toLocaleString('zh-CN');
}
function fmtDate(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  if (Number.isNaN(t) || t <= 0) return '—';
  return new Date(t).toLocaleString('zh-CN', { hour12: false });
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
const archiveColumns = [
  { dataIndex: 'month', key: 'month', title: '月份', width: 116 },
  { dataIndex: 'rowCount', key: 'rowCount', title: '行数', sorter: (a: LogArchive, b: LogArchive) => (a.rowCount ?? 0) - (b.rowCount ?? 0), width: 120 },
  { dataIndex: 'fileSize', key: 'fileSize', title: '文件大小', sorter: (a: LogArchive, b: LogArchive) => (a.fileSize ?? 0) - (b.fileSize ?? 0), width: 120 },
  { dataIndex: 'exportedBy', key: 'exportedBy', title: '来源', width: 90 },
  { dataIndex: 'createdAt', key: 'createdAt', title: '导出时间', sorter: (a: LogArchive, b: LogArchive) => Date.parse(a.createdAt) - Date.parse(b.createdAt) },
  { dataIndex: 'actions', key: 'actions', title: '操作', width: 130 },
];

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
    message.warning('保留天数需在 1–3650 之间');
    return;
  }
  if (logForm.archiveKeepMonths < 1 || logForm.archiveKeepMonths > 120) {
    message.warning('归档保留月数需在 1–120 之间');
    return;
  }
  logSaving.value = true;
  try {
    await updateLogSettings({
      retentionDays: logForm.retentionDays,
      archiveKeepMonths: logForm.archiveKeepMonths,
      autoExport: logForm.autoExport,
    });
    message.success('存储策略已保存并生效');
    await loadStats();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    logSaving.value = false;
  }
}

async function doExport() {
  const m = exportMonthInput.value.trim();
  if (!monthValid.value) {
    message.warning('请输入 YYYY-MM 格式的月份');
    return;
  }
  exporting.value = true;
  try {
    await exportLogArchive(m);
    message.success(`已导出 ${m} 归档`);
    await Promise.all([loadArchives(), loadStats()]);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导出失败');
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
    message.success(`已删除 ${month} 归档`);
    await Promise.all([loadArchives(), loadStats()]);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  } finally {
    busyMonth.value = '';
  }
}

async function downloadArchive(month: string) {
  busyMonth.value = month;
  try {
    await downloadLogArchive(month);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '下载失败');
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
        label="在线日志行数"
        :value="statsLoading ? '…' : fmtInt(stats?.totalRows)"
        icon="lucide:rows-3"
        tone="blue"
        :hint="stats?.earliestMonth ? `最早分区 ${stats.earliestMonth}` : '暂无在线分区'"
      />
      <StatCard
        label="磁盘占用"
        :value="fmtBytes(stats?.diskBytes)"
        icon="lucide:hard-drive"
        tone="cyan"
        :hint="`${partitions.length} 个活跃分区`"
      />
      <StatCard
        label="归档文件"
        :value="fmtInt(archives.length)"
        unit="份"
        icon="lucide:archive"
        tone="violet"
        :hint="latestArchive ? `最近归档 ${latestArchive}` : '尚未生成 Parquet 归档'"
      />
      <StatCard
        label="当前日志 TTL"
        :value="stats?.retentionDays ?? logForm.retentionDays"
        unit="天"
        icon="lucide:timer-reset"
        tone="green"
        :hint="`归档保留 ${logForm.archiveKeepMonths} 个月`"
      />
    </div>

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:timer-reset" />
            存储策略
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">ClickHouse TTL</span>
        </template>

        <div class="ipam-form-grid">
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">日志保留天数</span>
            <div class="flex items-center gap-2">
              <InputNumber
                v-model:value="logForm.retentionDays"
                :min="1"
                :max="3650"
                class="flex-1"
              />
              <span class="ipam-hint">天</span>
            </div>
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">归档保留月数</span>
            <div class="flex items-center gap-2">
              <InputNumber
                v-model:value="logForm.archiveKeepMonths"
                :min="1"
                :max="120"
                class="flex-1"
              />
              <span class="ipam-hint">个月</span>
            </div>
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">自动归档</span>
            <div class="flex h-8 items-center gap-2">
              <Switch v-model:checked="logForm.autoExport" size="small" />
              <span class="ipam-hint">
                {{ logForm.autoExport ? '每月 1 日 02:00 自动导出上月为 Parquet' : '仅支持手动导出' }}
              </span>
            </div>
          </div>
          <div class="ipam-form-hint ipam-form-field--wide">
            到期分区由 ClickHouse TTL 自动滚动删除；归档文件不占在线存储，删除归档不影响在线数据。
          </div>
          <div class="ipam-form-field--wide flex justify-end">
            <Button type="primary" :loading="logSaving" @click="saveSettings">
              <IconifyIcon icon="lucide:save" class="mr-1" />保存存储策略
            </Button>
          </div>
        </div>
      </Card>

      <Card class="lg:col-span-2">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:database" />
            在线存储
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">来自 ClickHouse system.parts</span>
        </template>

        <div class="ipam-metric-row">
          <div class="ipam-metric">
            <div class="ipam-metric__label">总行数</div>
            <div class="ipam-metric__value">{{ fmtInt(stats?.totalRows) }}</div>
          </div>
          <div class="ipam-metric">
            <div class="ipam-metric__label">磁盘占用</div>
            <div class="ipam-metric__value">{{ fmtBytes(stats?.diskBytes) }}</div>
          </div>
          <div class="ipam-metric">
            <div class="ipam-metric__label">最早月份</div>
            <div class="ipam-metric__value">{{ stats?.earliestMonth || '—' }}</div>
          </div>
          <div class="ipam-metric">
            <div class="ipam-metric__label">当前 TTL</div>
            <div class="ipam-metric__value">
              {{ stats?.retentionDays ?? logForm.retentionDays }}<small class="ml-1 text-xs font-normal">天</small>
            </div>
          </div>
        </div>

        <div v-if="partitions.length > 0" class="mt-4">
          <div class="mb-2 flex items-center justify-between text-xs text-gray-400">
            <span>按月分区 · 点击月份填入归档导出</span>
            <span>{{ partitions.length }} 个分区</span>
          </div>
          <div class="ipam-partition-list">
            <button
              v-for="p in partitions"
              :key="p.month"
              type="button"
              class="ipam-partition"
              :class="{ 'ipam-partition--active': exportMonthInput === p.month }"
              :title="`点击填入 ${p.month} 归档导出`"
              @click="pickMonth(p.month)"
            >
              <span class="ipam-partition__month ipam-mono">{{ p.month }}</span>
              <span class="ipam-partition__meta">
                {{ fmtInt(p.rowCount) }} 行 · {{ fmtBytes(p.diskBytes) }}
              </span>
              <span class="ipam-progress ipam-partition__bar">
                <i :style="{ width: `${Math.max(4, ((p.rowCount ?? 0) / partitionMax) * 100)}%` }"></i>
              </span>
            </button>
          </div>
        </div>
        <div v-else class="ipam-empty ipam-empty--compact mt-3">
          <div class="ipam-empty__title">暂无在线分区</div>
          <div class="ipam-empty__desc">ClickHouse 尚未写入日志，或存储统计接口暂不可达。</div>
        </div>
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:archive" />
          日志归档
          <em class="ipam-card-sub">{{ archives.length }} 份</em>
        </span>
      </template>
      <template #extra>
        <div class="ipam-card-tools">
          <Input
            v-model:value="keyword"
            allow-clear
            placeholder="筛选月份"
            style="width: 130px"
            size="small"
          />
          <Button size="small" :loading="archiveLoading" @click="loadArchives">
            <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
          </Button>
        </div>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">导出月份</span>
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
          <IconifyIcon icon="lucide:package-plus" class="mr-1 size-3.5" />导出该月
        </Button>
        <span class="ipam-toolbar__spacer"></span>
        <span class="ipam-hint">按月生成 Parquet 归档，不影响 ClickHouse 在线数据</span>
      </div>

      <Table
        :columns="archiveColumns"
        :data-source="archiveRows"
        :loading="archiveLoading"
        :locale="{ emptyText: archives.length === 0 ? '暂无归档：填写月份后点「导出该月」，或开启自动归档每月生成' : '没有匹配该月份的归档' }"
        :pagination="{
          pageSize: 8,
          position: ['bottomLeft'],
          showTotal: (t: number) => `共 ${t} 份`,
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
              {{ record.exportedBy === 'system' ? '自动' : '手动' }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'createdAt'">{{ fmtDate(record.createdAt) }}</template>
          <template v-else-if="column.key === 'actions'">
            <Button
              size="small"
              :loading="busyMonth === record.month"
              @click="downloadArchive(record.month)"
            >
              下载
            </Button>
            <Popconfirm
              title="确认删除该归档？"
              description="仅删除归档文件与记录，不影响 ClickHouse 在线数据。"
              ok-text="删除"
              cancel-text="取消"
              @confirm="removeArchive(record.month)"
            >
              <Button size="small" danger class="ml-2">删除</Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>
