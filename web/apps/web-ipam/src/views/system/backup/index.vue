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
import { $t } from '#/locales';

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

const KIND_LABEL = computed<Record<string, string>>(() => ({
  auto: $t('system.backup.kind.auto'),
  manual: $t('system.backup.kind.manual'),
  pre_upgrade: $t('system.backup.kind.pre_upgrade'),
}));
const STATUS_META = computed<Record<string, { color: string; label: string }>>(
  () => ({
    failed: { color: 'red', label: $t('system.backup.status.failed') },
    restoring: { color: 'orange', label: $t('system.backup.status.restoring') },
    running: { color: 'blue', label: $t('system.backup.status.running') },
    success: { color: 'green', label: $t('system.backup.status.success') },
  }),
);

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
  return Number.isNaN(t)
    ? '—'
    : new Date(t).toLocaleString(undefined, { hour12: false });
}

const successCount = computed(
  () => rows.value.filter((r) => r.status === 'success').length,
);
const lastSuccess = computed(() =>
  rows.value.find((r) => r.status === 'success'),
);
const monthCount = computed(() => (lastSuccess.value?.months ?? []).length);

async function load() {
  loading.value = true;
  try {
    const data = await listBackups();
    rows.value = data.items ?? [];
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.backup.msg.loadFailed'),
    );
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
    message.success($t('system.backup.msg.settingsSaved'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.backup.msg.saveFailed'),
    );
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
    message.success($t('system.backup.msg.created'));
    createOpen.value = false;
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.backup.msg.createFailed'),
    );
  } finally {
    creating.value = false;
  }
}

async function remove(rec: BackupRecord) {
  try {
    await deleteBackup(rec.id);
    message.success($t('system.backup.msg.deleted'));
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.backup.msg.deleteFailed'),
    );
  }
}

async function doRestore(rec: BackupRecord) {
  restoringId.value = rec.id;
  try {
    await restoreBackup(rec.id);
    message.success($t('system.backup.msg.restored'));
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.backup.msg.restoreFailed'),
    );
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
    message.error(
      error instanceof Error
        ? error.message
        : $t('system.backup.msg.downloadFailed'),
    );
  }
}

const columns = computed(() => [
  {
    dataIndex: 'startedAt',
    key: 'startedAt',
    title: $t('system.backup.col.startedAt'),
    width: 170,
  },
  {
    dataIndex: 'kind',
    key: 'kind',
    title: $t('system.backup.col.kind'),
    width: 90,
  },
  {
    dataIndex: 'status',
    key: 'status',
    title: $t('system.backup.col.status'),
    width: 110,
  },
  {
    dataIndex: 'sizeBytes',
    key: 'sizeBytes',
    title: $t('system.backup.col.sizeBytes'),
    width: 110,
  },
  {
    dataIndex: 'months',
    key: 'months',
    title: $t('system.backup.col.months'),
    minWidth: 150,
  },
  {
    dataIndex: 'note',
    key: 'note',
    title: $t('system.backup.col.note'),
    minWidth: 130,
  },
  {
    dataIndex: 'op',
    key: 'op',
    title: $t('system.backup.col.op'),
    width: 250,
    fixed: 'right' as const,
  },
]);

onMounted(() => {
  void load();
  void loadSettings();
  timer = setInterval(() => {
    if (
      rows.value.some((r) => r.status === 'running' || r.status === 'restoring')
    ) {
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
      :title="$t('system.backup.page.title')"
      :description="$t('system.backup.page.desc')"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.backup.actions.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openCreate">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />{{
            $t('system.backup.actions.createNow')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.backup.stat.total')"
        :value="rows.length"
        icon="lucide:archive"
        tone="blue"
        :hint="$t('system.backup.stat.totalHint', { n: successCount })"
      />
      <StatCard
        :label="$t('system.backup.stat.last')"
        :value="
          lastSuccess
            ? fmtTime(lastSuccess.startedAt)
            : $t('system.backup.stat.none')
        "
        icon="lucide:clock"
        tone="green"
        :hint="
          lastSuccess
            ? fmtBytes(lastSuccess.sizeBytes)
            : $t('system.backup.stat.createFirst')
        "
      />
      <StatCard
        :label="$t('system.backup.stat.logCoverage')"
        :value="monthCount"
        :unit="$t('system.backup.stat.logUnit')"
        icon="lucide:calendar-days"
        tone="cyan"
        :hint="$t('system.backup.stat.logHint')"
      />
      <StatCard
        :label="$t('system.backup.stat.policy')"
        :value="
          settings.enabled
            ? $t('system.backup.stat.daily', { hour: settings.scheduleHour })
            : $t('system.backup.stat.off')
        "
        icon="lucide:timer-reset"
        :tone="settings.enabled ? 'green' : 'violet'"
        :hint="
          $t('system.backup.stat.retention', { n: settings.retentionCount })
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list" />{{ $t('system.backup.list.title') }}
          <em class="ipam-card-sub">{{
            $t('system.backup.list.count', { n: rows.length })
          }}</em>
        </span>
      </template>
      <Table
        :columns="columns"
        :data-source="rows"
        :loading="loading"
        :locale="{ emptyText: $t('system.backup.list.empty') }"
        :pagination="false"
        :row-key="(r: BackupRecord) => r.id"
        :scroll="{ x: 1000 }"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'startedAt'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{
                fmtTime(record.startedAt)
              }}</span>
              <span v-if="record.finishedAt" class="ipam-cell-meta">
                {{
                  $t('system.backup.list.finished', {
                    time: fmtTime(record.finishedAt),
                  })
                }}
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'kind'">
            <Tag class="m-0">{{ KIND_LABEL[record.kind] ?? record.kind }}</Tag>
          </template>
          <template v-else-if="column.key === 'status'">
            <Tag
              class="m-0"
              :color="STATUS_META[record.status]?.color ?? 'default'"
            >
              {{ STATUS_META[record.status]?.label ?? record.status }}
            </Tag>
            <div
              v-if="record.error"
              class="ipam-cell-meta"
              :title="record.error"
            >
              {{ record.error }}
            </div>
          </template>
          <template v-else-if="column.key === 'sizeBytes'">
            {{ fmtBytes(record.sizeBytes) }}
          </template>
          <template v-else-if="column.key === 'months'">
            <div class="flex flex-wrap gap-1">
              <Tag v-for="m in record.months ?? []" :key="m" class="m-0">
                {{ m }}
              </Tag>
              <span v-if="(record.months ?? []).length === 0" class="ipam-hint"
                >—</span
              >
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
                <IconifyIcon icon="lucide:download" class="mr-1 size-3.5" />{{
                  $t('system.backup.actions.download')
                }}
              </Button>
              <Popconfirm
                :title="$t('system.backup.confirm.restoreTitle')"
                :description="$t('system.backup.confirm.restoreDesc')"
                :ok-text="$t('system.backup.confirm.restoreOk')"
                :cancel-text="$t('system.backup.confirm.cancel')"
                @confirm="doRestore(record as BackupRecord)"
              >
                <Button
                  size="small"
                  danger
                  :loading="restoringId === record.id"
                  :disabled="record.status !== 'success'"
                >
                  {{ $t('system.backup.actions.restore') }}
                </Button>
              </Popconfirm>
              <Popconfirm
                :title="$t('system.backup.confirm.deleteTitle')"
                :description="$t('system.backup.confirm.deleteDesc')"
                :ok-text="$t('system.backup.confirm.deleteOk')"
                :cancel-text="$t('system.backup.confirm.cancel')"
                @confirm="remove(record as BackupRecord)"
              >
                <Button
                  size="small"
                  danger
                  :disabled="
                    record.status === 'running' || record.status === 'restoring'
                  "
                >
                  {{ $t('system.backup.actions.delete') }}
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
          <IconifyIcon icon="lucide:timer-reset" />{{
            $t('system.backup.policy.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{
          $t('system.backup.policy.scheduleHint')
        }}</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.backup.policy.enabled')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.enabled" size="small" />
            <span class="ipam-hint">{{
              settings.enabled
                ? $t('system.backup.policy.on')
                : $t('system.backup.policy.off')
            }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.backup.policy.hour')
          }}</span>
          <InputNumber
            v-model:value="settings.scheduleHour"
            :min="0"
            :max="23"
            style="width: 100%"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('system.backup.policy.retention')
          }}</span>
          <InputNumber
            v-model:value="settings.retentionCount"
            :min="1"
            :max="90"
            style="width: 100%"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.backup.policy.retentionHint') }}
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ $t('system.backup.policy.dataHint') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="saveSettings">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.backup.actions.save')
          }}
        </Button>
      </div>
    </Card>

    <Modal
      v-model:open="createOpen"
      :title="$t('system.backup.modal.title')"
      :ok-text="$t('system.backup.modal.ok')"
      :cancel-text="$t('system.backup.modal.cancel')"
      :confirm-loading="creating"
      @ok="submitCreate"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.backup.modal.note')
          }}</span>
          <Input
            v-model:value="note"
            :placeholder="$t('system.backup.modal.notePlaceholder')"
            :maxlength="200"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('system.backup.modal.hint') }}
        </div>
      </div>
    </Modal>
  </div>
</template>
