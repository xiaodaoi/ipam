<script setup lang="ts">
import type { BackupRecord, BackupSettings } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { useAccessStore } from '@vben/stores';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Modal,
  Popconfirm,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  createBackup,
  deleteBackup,
  getBackupSettings,
  listBackups,
  restoreBackup,
  updateBackupSettings,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

defineOptions({ name: 'SystemBackup' });

const accessStore = useAccessStore();
const rows = ref<BackupRecord[]>([]);
const loading = ref(false);
const creating = ref(false);
const saving = ref(false);
const restoringId = ref('');
const createOpen = ref(false);
const note = ref('');
const settings = reactive<BackupSettings>({
  enabled: true,
  retentionCount: 7,
  scheduleHour: 3,
});
let timer: ReturnType<typeof setInterval> | undefined;

const KIND_LABEL: Record<string, string> = {
  auto: '定时',
  manual: '手动',
  pre_upgrade: '升级前',
};
const STATUS_META: Record<string, { color: string; label: string }> = {
  failed: { color: 'red', label: '失败' },
  restoring: { color: 'orange', label: '恢复中' },
  running: { color: 'blue', label: '进行中' },
  success: { color: 'green', label: '成功' },
};

function fmtBytes(v?: null | number): string {
  if (!v) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let n = v;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n >= 10 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}
function fmtTime(v?: string): string {
  if (!v) return '—';
  const t = Date.parse(v);
  return Number.isNaN(t) ? '—' : new Date(t).toLocaleString('zh-CN', { hour12: false });
}

const successCount = computed(() => rows.value.filter((r) => r.status === 'success').length);
const lastSuccess = computed(() => rows.value.find((r) => r.status === 'success'));
const monthCount = computed(() => (lastSuccess.value?.months ?? []).length);

async function load() {
  loading.value = true;
  try {
    const data = await listBackups();
    rows.value = data.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '备份列表加载失败');
  } finally {
    loading.value = false;
  }
}

async function loadSettings() {
  try {
    const s = await getBackupSettings();
    settings.enabled = s.enabled;
    settings.scheduleHour = s.scheduleHour;
    settings.retentionCount = s.retentionCount;
  } catch {
    // 保留默认值
  }
}

async function saveSettings() {
  saving.value = true;
  try {
    await updateBackupSettings({
      enabled: settings.enabled,
      retentionCount: settings.retentionCount,
      scheduleHour: settings.scheduleHour,
    });
    message.success('备份策略已保存');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    saving.value = false;
  }
}

function openCreate() {
  note.value = '';
  createOpen.value = true;
}

async function submitCreate() {
  creating.value = true;
  try {
    await createBackup(note.value.trim() || undefined);
    message.success('备份任务已创建，正在后台执行');
    createOpen.value = false;
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '创建备份失败');
  } finally {
    creating.value = false;
  }
}

async function remove(rec: BackupRecord) {
  try {
    await deleteBackup(rec.id);
    message.success('备份已删除');
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
}

async function doRestore(rec: BackupRecord) {
  restoringId.value = rec.id;
  try {
    await restoreBackup(rec.id);
    message.success('恢复完成；如页面数据异常请刷新');
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '恢复失败');
  } finally {
    restoringId.value = '';
  }
}

async function download(rec: BackupRecord) {
  const base = (import.meta.env.VITE_GLOB_API_URL as string) || '/api/v1';
  const token = accessStore.accessToken;
  try {
    const res = await fetch(`${base}/system/backups/${rec.id}/download`, {
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `backup-${rec.id}.tar.gz`;
    a.click();
    URL.revokeObjectURL(url);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '下载失败');
  }
}

const columns = [
  { dataIndex: 'startedAt', key: 'startedAt', title: '时间', width: 170 },
  { dataIndex: 'kind', key: 'kind', title: '类型', width: 90 },
  { dataIndex: 'status', key: 'status', title: '状态', width: 110 },
  { dataIndex: 'sizeBytes', key: 'sizeBytes', title: '大小', width: 110 },
  { dataIndex: 'months', key: 'months', title: '日志月份', minWidth: 150 },
  { dataIndex: 'note', key: 'note', title: '备注', minWidth: 130 },
  { dataIndex: 'op', key: 'op', title: '操作', width: 250, fixed: 'right' as const },
];

onMounted(() => {
  void load();
  void loadSettings();
  timer = setInterval(() => {
    if (rows.value.some((r) => r.status === 'running' || r.status === 'restoring')) {
      void load();
    }
  }, 5000);
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:archive-restore"
      title="备份与恢复"
      description="备份 PostgreSQL、配置目录与 ClickHouse 日志；支持下载、恢复与每日自动策略。恢复会覆盖当前数据，请在维护窗口执行。"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />立即备份
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="备份总数"
        :value="rows.length"
        icon="lucide:archive"
        tone="blue"
        :hint="`成功 ${successCount} 份`"
      />
      <StatCard
        label="最近成功"
        :value="lastSuccess ? fmtTime(lastSuccess.startedAt) : '暂无'"
        icon="lucide:clock"
        tone="green"
        :hint="lastSuccess ? fmtBytes(lastSuccess.sizeBytes) : '创建第一份备份'"
      />
      <StatCard
        label="日志覆盖"
        :value="monthCount"
        unit="月"
        icon="lucide:calendar-days"
        tone="cyan"
        hint="最近成功备份包含的日志月份"
      />
      <StatCard
        label="自动策略"
        :value="settings.enabled ? `每日 ${settings.scheduleHour}:00` : '已关闭'"
        icon="lucide:timer-reset"
        :tone="settings.enabled ? 'green' : 'violet'"
        :hint="`保留 ${settings.retentionCount} 份`"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list" />备份记录
          <em class="ipam-card-sub">{{ rows.length }} 份</em>
        </span>
      </template>
      <Table
        :columns="columns"
        :data-source="rows"
        :loading="loading"
        :locale="{ emptyText: '暂无备份，点击右上角「立即备份」创建' }"
        :pagination="false"
        :row-key="(r: BackupRecord) => r.id"
        :scroll="{ x: 1000 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'startedAt'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ fmtTime(record.startedAt) }}</span>
              <span v-if="record.finishedAt" class="ipam-cell-meta">
                完成 {{ fmtTime(record.finishedAt) }}
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'kind'">
            <Tag class="m-0">{{ KIND_LABEL[record.kind] ?? record.kind }}</Tag>
          </template>
          <template v-else-if="column.key === 'status'">
            <Tag class="m-0" :color="STATUS_META[record.status]?.color ?? 'default'">
              {{ STATUS_META[record.status]?.label ?? record.status }}
            </Tag>
            <div v-if="record.error" class="ipam-cell-meta" :title="record.error">
              {{ record.error }}
            </div>
          </template>
          <template v-else-if="column.key === 'sizeBytes'">
            {{ fmtBytes(record.sizeBytes) }}
          </template>
          <template v-else-if="column.key === 'months'">
            <div class="flex flex-wrap gap-1">
              <Tag v-for="m in record.months ?? []" :key="m" class="m-0">{{ m }}</Tag>
              <span v-if="(record.months ?? []).length === 0" class="ipam-hint">—</span>
            </div>
          </template>
          <template v-else-if="column.key === 'note'">
            <span class="ipam-hint">{{ record.note || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex flex-wrap items-center gap-1">
              <Button
                size="small"
                :disabled="record.status !== 'success'"
                @click="download(record as BackupRecord)"
              >
                <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />下载
              </Button>
              <Popconfirm
                title="确认从该备份恢复？"
                description="将覆盖当前 PostgreSQL 数据与 ClickHouse 日志，期间管理页面可能短暂不可用。"
                ok-text="恢复"
                cancel-text="取消"
                @confirm="doRestore(record as BackupRecord)"
              >
                <Button
                  size="small"
                  danger
                  :loading="restoringId === record.id"
                  :disabled="record.status !== 'success'"
                >
                  恢复
                </Button>
              </Popconfirm>
              <Popconfirm
                title="确认删除该备份？"
                description="备份文件与记录将一并删除，不可恢复。"
                ok-text="删除"
                cancel-text="取消"
                @confirm="remove(record as BackupRecord)"
              >
                <Button
                  size="small"
                  danger
                  :disabled="record.status === 'running' || record.status === 'restoring'"
                >
                  删除
                </Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:timer-reset" />自动备份策略
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">每日检查一次，按 UTC 执行</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">启用自动备份</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.enabled" size="small" />
            <span class="ipam-hint">{{ settings.enabled ? '已启用' : '已关闭' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">执行小时（UTC）</span>
          <InputNumber v-model:value="settings.scheduleHour" :min="0" :max="23" style="width: 100%" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">保留份数</span>
          <InputNumber v-model:value="settings.retentionCount" :min="1" :max="90" style="width: 100%" />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          超出保留份数的旧备份会被自动删除；升级前建议手动创建一份并下载留存。
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">自动备份包含 PG、配置目录与 ClickHouse 日志 Parquet</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="saveSettings">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存策略
        </Button>
      </div>
    </Card>

    <Modal
      v-model:open="createOpen"
      title="立即备份"
      ok-text="开始备份"
      cancel-text="取消"
      :confirm-loading="creating"
      @ok="submitCreate"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">备注（可选）</span>
          <Input v-model:value="note" placeholder="如 升级前快照" :maxlength="200" />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          备份在后台执行；状态每 5 秒自动刷新。日志量大时可能需要数分钟。
        </div>
      </div>
    </Modal>
  </div>
</template>
