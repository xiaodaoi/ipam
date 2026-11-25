<script setup lang="ts">
import type {
  AlertPolicy,
  IntelSourceRevisionRow,
  IntelSourceRow,
  IntelSourceRunRow,
} from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Checkbox,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Switch,
  Table,
  TabPane,
  Tabs,
  Tag,
} from 'ant-design-vue';

import {
  createIntelSource,
  deleteIntelSource,
  listAlertPolicies,
  listIntelSourceRevisions,
  listIntelSourceRuns,
  listIntelSources,
  rollbackIntelSourceRevision,
  syncIntelSource,
  updateIntelSource,
} from '#/api/ipam';
import { $t } from '#/locales';

// 情报源管理（M6-039）：全部默认关闭，启用需确认许可；密钥只写不读。
// 部分上游（abuse.ch v2）要求密钥放在 URL 路径：URL 内用 {secret} 占位。
function needsSecret(src: IntelSourceRow): boolean {
  return !!src.authHeader || (src.url ?? '').includes('{secret}');
}
const sources = ref<IntelSourceRow[]>([]);
const policiesByKey = ref<Record<string, AlertPolicy>>({});
const loading = ref(false);
const syncingId = ref('');
const saving = ref(false);

const enabledCount = computed(
  () => sources.value.filter((s) => s.enabled).length,
);

async function load() {
  loading.value = true;
  try {
    const res = await listIntelSources();
    sources.value = res.items ?? [];
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.loadFailed'),
    );
  } finally {
    loading.value = false;
  }
  // 告警策略需要 system:read；无权限时静默跳过，不影响情报源管理。
  try {
    const res = await listAlertPolicies();
    const map: Record<string, AlertPolicy> = {};
    for (const p of res.items ?? []) {
      if (p.scopeType === 'intel_source') map[p.scopeKey] = p;
    }
    policiesByKey.value = map;
  } catch {
    policiesByKey.value = {};
  }
}

const POLICY_MODE_COLOR: Record<string, string> = {
  digest: 'blue',
  notify: 'green',
  off: 'default',
  silent: 'purple',
};

function policyFor(row: IntelSourceRow): AlertPolicy | undefined {
  return policiesByKey.value[row.key];
}

// ── 启用（许可确认 + 可选密钥）──
const enableTarget = ref<IntelSourceRow | null>(null);
const ackChecked = ref(false);
const enableSecret = ref('');
const [EnableModal, enableModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.blocklist.intel.src.enableTitle'),
  confirmText: $t('dns.blocklist.intel.src.enable'),
  onConfirm: () => confirmEnable(),
});
function openEnable(src: IntelSourceRow) {
  enableTarget.value = src;
  ackChecked.value = !!src.acknowledgedAt;
  enableSecret.value = '';
  enableModalApi.open();
}
async function confirmEnable() {
  const src = enableTarget.value;
  if (!src) return;
  if (src.requiresAck && !ackChecked.value) {
    message.warning($t('dns.blocklist.intel.src.ackRequired'));
    return;
  }
  const needSecret = needsSecret(src) && !src.hasSecret;
  if (needSecret && !enableSecret.value.trim()) {
    message.warning($t('dns.blocklist.intel.src.secretRequired'));
    return;
  }
  saving.value = true;
  try {
    await updateIntelSource(src.id, {
      ackLicense: true,
      enabled: true,
      secret: enableSecret.value.trim() || undefined,
    });
    message.success($t('dns.blocklist.intel.src.msgEnabled'));
    enableModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

async function onToggle(src: IntelSourceRow, checked: boolean) {
  if (checked) {
    openEnable(src);
    return;
  }
  try {
    await updateIntelSource(src.id, { enabled: false });
    message.success($t('dns.blocklist.intel.src.msgDisabled'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.saveFailed'),
    );
  }
  await load();
}

// ── 编辑 ──
const editForm = ref<{
  clearSecret: boolean;
  id: string;
  intervalMin: number;
  layer: 'broad' | 'hot';
  maxSharePct: number;
  name: string;
  secret: string;
  url: string;
}>({
  clearSecret: false,
  id: '',
  intervalMin: 360,
  layer: 'hot',
  maxSharePct: 60,
  name: '',
  secret: '',
  url: '',
});
const editBuiltin = ref(false);
const editNeedsAuth = ref(false);
const editAuthHeader = ref('');
const [EditModal, editModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.blocklist.intel.src.editTitle'),
  confirmText: $t('dns.common.save'),
  onConfirm: () => saveEdit(),
});
function openEdit(src: IntelSourceRow) {
  editBuiltin.value = !!src.builtin;
  editNeedsAuth.value = needsSecret(src);
  editAuthHeader.value = src.authHeader ?? '';
  editForm.value = {
    clearSecret: false,
    id: src.id,
    intervalMin: Math.max(5, Math.round((src.updateInterval || 3600) / 60)),
    layer: src.layer,
    maxSharePct: src.maxSharePct ?? 60,
    name: src.name,
    secret: '',
    url: src.url ?? '',
  };
  editModalApi.open();
}
async function saveEdit() {
  const f = editForm.value;
  if (!f.url.trim()) {
    message.warning($t('dns.blocklist.intel.src.urlRequired'));
    return;
  }
  saving.value = true;
  try {
    await updateIntelSource(f.id, {
      clearSecret: f.clearSecret,
      layer: f.layer,
      maxSharePct: Math.round(f.maxSharePct),
      name: f.name.trim(),
      secret: f.secret.trim() || undefined,
      updateInterval: Math.round(f.intervalMin * 60),
      url: f.url.trim(),
    });
    message.success($t('dns.blocklist.intel.src.msgSaved'));
    editModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

// ── 新增自定义源 ──
const createForm = ref<{
  intervalMin: number;
  layer: 'broad' | 'hot';
  maxSharePct: number;
  name: string;
  url: string;
}>({ intervalMin: 360, layer: 'hot', maxSharePct: 60, name: '', url: '' });
const [CreateModal, createModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.blocklist.intel.src.createTitle'),
  confirmText: $t('dns.common.create'),
  onConfirm: () => saveCreate(),
});
function openCreate() {
  createForm.value = {
    intervalMin: 360,
    layer: 'hot',
    maxSharePct: 60,
    name: '',
    url: '',
  };
  createModalApi.open();
}
async function saveCreate() {
  const f = createForm.value;
  if (!f.name.trim() || !f.url.trim()) {
    message.warning($t('dns.blocklist.intel.src.nameUrlRequired'));
    return;
  }
  saving.value = true;
  try {
    await createIntelSource({
      layer: f.layer,
      maxSharePct: Math.round(f.maxSharePct),
      name: f.name.trim(),
      updateInterval: Math.round(f.intervalMin * 60),
      url: f.url.trim(),
    });
    message.success($t('dns.blocklist.intel.src.msgCreated'));
    createModalApi.close();
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

async function syncNow(src: IntelSourceRow) {
  syncingId.value = src.id;
  try {
    await syncIntelSource(src.id);
    message.success($t('dns.blocklist.intel.src.msgSynced'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.statusFailed'),
    );
  } finally {
    syncingId.value = '';
    await load();
  }
}

async function remove(src: IntelSourceRow) {
  try {
    await deleteIntelSource(src.id);
    message.success($t('dns.blocklist.intel.src.msgDeleted'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.deleteFailed'),
    );
  }
  await load();
}

// ── 运行记录与版本回滚（M6-039 P2）──
const opsTarget = ref<IntelSourceRow | null>(null);
const opsTab = ref('runs');
const runs = ref<IntelSourceRunRow[]>([]);
const revisions = ref<IntelSourceRevisionRow[]>([]);
const opsLoading = ref(false);
const rollingBack = ref(0);
const [OpsModal, opsModalApi] = useVbenModal({
  draggable: true,
  footer: false,
  title: $t('dns.blocklist.intel.src.opsTitle'),
});

async function openOps(src: IntelSourceRow) {
  opsTarget.value = src;
  opsTab.value = 'runs';
  runs.value = [];
  revisions.value = [];
  opsModalApi.open();
  await loadOps(src.id);
}

async function loadOps(id?: string) {
  const sourceId = id ?? opsTarget.value?.id;
  if (!sourceId) return;
  opsLoading.value = true;
  try {
    const [runRes, revRes] = await Promise.all([
      listIntelSourceRuns(sourceId, 30),
      listIntelSourceRevisions(sourceId, 30),
    ]);
    runs.value = runRes.items ?? [];
    revisions.value = revRes.items ?? [];
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.opsFailed'),
    );
  } finally {
    opsLoading.value = false;
  }
}

async function rollbackRevision(rev: IntelSourceRevisionRow) {
  const src = opsTarget.value;
  if (!src) return;
  rollingBack.value = rev.revision;
  try {
    await rollbackIntelSourceRevision(src.id, rev.revision);
    message.success($t('dns.blocklist.intel.src.msgRolledBack'));
    await Promise.all([load(), loadOps(src.id)]);
    const latest = sources.value.find((s) => s.id === src.id);
    if (latest) opsTarget.value = latest;
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.blocklist.intel.src.rollbackFailed'),
    );
  } finally {
    rollingBack.value = 0;
  }
}

function fmtDuration(ms?: null | number) {
  if (!ms || ms <= 0) return '—';
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}

const LAYER_TEXT = computed<Record<string, string>>(() => ({
  broad: $t('dns.blocklist.intel.src.layerBroad'),
  hot: $t('dns.blocklist.intel.src.layerHot'),
}));
const STATUS_TEXT = computed<Record<string, string>>(() => ({
  failed: $t('dns.blocklist.intel.src.statusFailed'),
  never: $t('dns.blocklist.intel.src.statusNever'),
  ok: $t('dns.blocklist.intel.src.statusOk'),
}));
const STATUS_COLOR: Record<string, string> = {
  failed: 'red',
  never: 'default',
  ok: 'green',
  rolled_back: 'blue',
};

const TRIGGER_TEXT = computed<Record<string, string>>(() => ({
  enable: $t('dns.blocklist.intel.src.triggerEnable'),
  manual: $t('dns.blocklist.intel.src.triggerManual'),
  rollback: $t('dns.blocklist.intel.src.triggerRollback'),
  schedule: $t('dns.blocklist.intel.src.triggerSchedule'),
}));
const RUN_STATUS_TEXT = computed<Record<string, string>>(() => ({
  failed: $t('dns.blocklist.intel.src.statusFailed'),
  ok: $t('dns.blocklist.intel.src.statusOk'),
  rolled_back: $t('dns.blocklist.intel.src.statusRolledBack'),
}));

const runColumns = computed(() => [
  {
    dataIndex: 'startedAt',
    key: 'startedAt',
    width: 170,
    title: $t('dns.blocklist.intel.src.colStarted'),
  },
  {
    dataIndex: 'status',
    key: 'status',
    width: 100,
    title: $t('dns.blocklist.intel.src.colStatus'),
  },
  {
    dataIndex: 'trigger',
    key: 'trigger',
    width: 90,
    title: $t('dns.blocklist.intel.src.colTrigger'),
  },
  {
    dataIndex: 'durationMs',
    key: 'durationMs',
    width: 90,
    title: $t('dns.blocklist.intel.src.colDuration'),
  },
  {
    dataIndex: 'entries',
    key: 'entries',
    width: 90,
    title: $t('dns.blocklist.intel.src.colEntries'),
  },
  {
    dataIndex: 'added',
    key: 'added',
    width: 80,
    title: $t('dns.blocklist.intel.src.colAdded'),
  },
  {
    dataIndex: 'removed',
    key: 'removed',
    width: 80,
    title: $t('dns.blocklist.intel.src.colRemoved'),
  },
  {
    dataIndex: 'excluded',
    key: 'excluded',
    width: 110,
    title: $t('dns.blocklist.intel.src.colExcluded'),
  },
  {
    dataIndex: 'revision',
    key: 'revision',
    width: 80,
    title: $t('dns.blocklist.intel.src.colRevision'),
  },
]);

const revisionColumns = computed(() => [
  {
    dataIndex: 'revision',
    key: 'revision',
    width: 90,
    title: $t('dns.blocklist.intel.src.colRevision'),
  },
  {
    dataIndex: 'entries',
    key: 'entries',
    width: 110,
    title: $t('dns.blocklist.intel.src.colEntries'),
  },
  {
    dataIndex: 'active',
    key: 'active',
    width: 100,
    title: $t('dns.blocklist.intel.src.colActive'),
  },
  {
    dataIndex: 'createdAt',
    key: 'createdAt',
    width: 180,
    title: $t('dns.blocklist.intel.src.colStarted'),
  },
  {
    key: 'op',
    width: 110,
    title: $t('dns.common.actions'),
  },
]);

function fmtTime(v?: null | string) {
  if (!v) return '—';
  const date = new Date(v);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1970) return '—';
  return date.toLocaleString(undefined, { hour12: false });
}

const columns = computed(() => [
  {
    dataIndex: 'name',
    key: 'name',
    minWidth: 260,
    title: $t('dns.blocklist.intel.src.colName'),
  },
  {
    dataIndex: 'layer',
    key: 'layer',
    width: 150,
    title: $t('dns.blocklist.intel.src.colLayer'),
  },
  {
    dataIndex: 'license',
    key: 'license',
    minWidth: 200,
    title: $t('dns.blocklist.intel.src.colLicense'),
  },
  {
    key: 'policy',
    width: 130,
    title: $t('alerting.policies.title'),
  },
  {
    dataIndex: 'status',
    key: 'status',
    width: 190,
    title: $t('dns.blocklist.intel.src.colStatus'),
  },
  {
    dataIndex: 'entries',
    key: 'entries',
    width: 100,
    title: $t('dns.blocklist.intel.src.colEntries'),
  },
  {
    dataIndex: 'lastSyncAt',
    key: 'lastSyncAt',
    width: 180,
    title: $t('dns.blocklist.intel.src.colLastSync'),
  },
  { key: 'op', width: 280, title: $t('dns.common.actions') },
]);

onMounted(load);
defineExpose({ load });
</script>

<template>
  <Card>
    <template #title>
      <span class="ipam-card-title">
        <IconifyIcon icon="lucide:rss" />
        {{ $t('dns.blocklist.intel.src.title') }}
        <em class="ipam-card-sub">{{
          $t('dns.blocklist.intel.src.sub', {
            enabled: enabledCount,
            total: sources.length,
          })
        }}</em>
      </span>
    </template>
    <template #extra>
      <span class="ipam-hint">{{ $t('dns.blocklist.intel.src.hint') }}</span>
    </template>

    <div class="ipam-toolbar mb-3">
      <div class="ipam-toolbar__spacer"></div>
      <Button size="small" @click="load">
        <IconifyIcon icon="lucide:refresh-cw" class="mr-1" />{{
          $t('dns.common.refresh')
        }}
      </Button>
      <Button type="primary" size="small" @click="openCreate">
        <IconifyIcon icon="lucide:plus" class="mr-1" />{{
          $t('dns.blocklist.intel.src.add')
        }}
      </Button>
    </div>

    <Table
      :columns="columns"
      :data-source="sources"
      :loading="loading"
      :locale="{ emptyText: $t('dns.blocklist.intel.src.empty') }"
      :pagination="false"
      row-key="id"
      size="small"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'name'">
          <div class="ipam-cell-title">
            {{ record.name }}
            <Tag v-if="record.builtin" class="ml-1" color="default">
              {{ $t('dns.blocklist.intel.src.builtin') }}
            </Tag>
            <Tag v-else class="ml-1" color="blue">
              {{ $t('dns.blocklist.intel.src.custom') }}
            </Tag>
          </div>
          <div class="ipam-cell-meta ipam-mono">{{ record.key }}</div>
        </template>
        <template v-else-if="column.key === 'layer'">
          <Tag class="m-0" color="geekblue">
            {{ LAYER_TEXT[record.layer] ?? record.layer }}
          </Tag>
        </template>
        <template v-else-if="column.key === 'policy'">
          <Tag
            v-if="policyFor(record as IntelSourceRow)"
            :color="
              POLICY_MODE_COLOR[
                policyFor(record as IntelSourceRow)?.notifyMode ?? 'notify'
              ] ?? 'default'
            "
            class="m-0"
          >
            {{
              $t(
                `alerting.mode.${policyFor(record as IntelSourceRow)?.notifyMode ?? 'notify'}`,
              )
            }}
          </Tag>
          <span v-else class="ipam-hint">{{ $t('alerting.common.none') }}</span>
        </template>
        <template v-else-if="column.key === 'license'">
          <div class="ipam-cell-meta">{{ record.license || '—' }}</div>
          <Tag class="mt-1" :color="record.commercialOk ? 'green' : 'orange'">
            {{
              record.commercialOk
                ? $t('dns.blocklist.intel.src.commercialYes')
                : $t('dns.blocklist.intel.src.commercialNo')
            }}
          </Tag>
        </template>
        <template v-else-if="column.key === 'status'">
          <Switch
            :checked="record.enabled"
            :loading="saving && enableTarget?.id === record.id"
            checked-children="ON"
            un-checked-children="OFF"
            size="small"
            @change="
              (v: boolean | number | string) =>
                onToggle(record as IntelSourceRow, !!v)
            "
          />
          <div class="mt-1">
            <Tag
              class="m-0"
              :color="STATUS_COLOR[record.lastStatus ?? 'never'] ?? 'default'"
            >
              {{
                STATUS_TEXT[record.lastStatus ?? 'never'] ?? record.lastStatus
              }}
            </Tag>
          </div>
          <div
            v-if="record.lastStatus === 'failed' && record.lastError"
            class="ipam-cell-meta mt-1"
            :title="record.lastError"
          >
            {{ record.lastError }}
          </div>
          <div
            v-else-if="record.lastStatus === 'ok'"
            class="ipam-cell-meta mt-1 ipam-mono"
          >
            {{ fmtDuration(record.lastDurationMs) }} ·
            <span class="text-green-600">+{{ record.lastAdded ?? 0 }}</span>
            /
            <span class="text-red-500">-{{ record.lastRemoved ?? 0 }}</span>
            <template v-if="record.lastExcluded">
              · {{ $t('dns.blocklist.intel.src.colExcluded') }}
              {{ record.lastExcluded }}
            </template>
          </div>
        </template>
        <template v-else-if="column.key === 'entries'">
          <span class="ipam-chip" data-tone="blue">{{
            $t('dns.common.count', { n: record.entries ?? 0 })
          }}</span>
          <div class="ipam-cell-meta mt-1">
            {{ $t('dns.blocklist.intel.src.colMaxShare') }}:
            {{ record.maxSharePct ?? 0 }}%
          </div>
        </template>
        <template v-else-if="column.key === 'lastSyncAt'">
          <span class="ipam-cell-meta">{{ fmtTime(record.lastSyncAt) }}</span>
        </template>
        <template v-else-if="column.key === 'op'">
          <div class="flex flex-wrap gap-1">
            <Button
              :disabled="!record.enabled"
              :loading="syncingId === record.id"
              size="small"
              @click="syncNow(record as IntelSourceRow)"
            >
              {{ $t('dns.blocklist.intel.src.sync') }}
            </Button>
            <Button size="small" @click="openOps(record as IntelSourceRow)">
              {{ $t('dns.blocklist.intel.src.ops') }}
            </Button>
            <Button size="small" @click="openEdit(record as IntelSourceRow)">
              {{ $t('dns.blocklist.intel.src.edit') }}
            </Button>
            <Popconfirm
              v-if="!record.builtin"
              :cancel-text="$t('dns.common.cancel')"
              :description="$t('dns.blocklist.intel.src.deleteDesc')"
              :ok-text="$t('dns.common.delete')"
              :title="$t('dns.blocklist.intel.src.deleteTitle')"
              @confirm="remove(record as IntelSourceRow)"
            >
              <Button danger size="small">
                {{ $t('dns.common.delete') }}
              </Button>
            </Popconfirm>
          </div>
        </template>
      </template>
    </Table>
  </Card>

  <!-- 启用确认：许可 + 可选密钥 -->
  <EnableModal class="w-[600px]">
    <div class="ipam-form-grid">
      <div class="ipam-form-field--wide">
        <div class="ipam-cell-title">{{ enableTarget?.name }}</div>
        <div class="ipam-form-hint">
          {{ enableTarget?.license || '—' }}
          <a
            v-if="enableTarget?.licenseUrl"
            :href="enableTarget.licenseUrl"
            class="ml-1"
            rel="noreferrer"
            target="_blank"
          >
            {{ $t('dns.blocklist.intel.src.licenseLink') }}
          </a>
        </div>
        <Tag
          v-if="enableTarget?.builtin && !enableTarget.commercialOk"
          class="mt-2"
          color="orange"
        >
          {{ $t('dns.blocklist.intel.src.commercialWarn') }}
        </Tag>
      </div>
      <label
        v-if="enableTarget?.requiresAck"
        class="ipam-form-field--wide flex items-start gap-2"
      >
        <Checkbox v-model:checked="ackChecked">
          {{ $t('dns.blocklist.intel.src.ackLabel') }}
        </Checkbox>
      </label>
      <label
        v-if="enableTarget && needsSecret(enableTarget)"
        class="ipam-form-field ipam-form-field--wide"
      >
        <span class="ipam-form-field__label">
          {{
            enableTarget.authHeader
              ? $t('dns.blocklist.intel.src.secretLabel', {
                  header: enableTarget.authHeader,
                })
              : $t('dns.blocklist.intel.src.secretLabelUrl')
          }}
        </span>
        <Input.Password
          v-model:value="enableSecret"
          autocomplete="new-password"
          :placeholder="
            enableTarget.hasSecret
              ? $t('dns.blocklist.intel.src.secretKeep')
              : $t('dns.blocklist.intel.src.secretPlaceholder')
          "
        />
      </label>
      <div class="ipam-form-hint ipam-form-field--wide">
        {{ $t('dns.blocklist.intel.src.enableHint') }}
      </div>
    </div>
  </EnableModal>

  <!-- 编辑 -->
  <EditModal class="w-[640px]">
    <div class="ipam-form-grid">
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.nameLabel')
        }}</span>
        <Input v-model:value="editForm.name" :disabled="editBuiltin" />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.layerLabel')
        }}</span>
        <Select
          v-model:value="editForm.layer"
          :options="[
            { label: $t('dns.blocklist.intel.src.layerHot'), value: 'hot' },
            { label: $t('dns.blocklist.intel.src.layerBroad'), value: 'broad' },
          ]"
        />
      </label>
      <label class="ipam-form-field ipam-form-field--wide">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.urlLabel')
        }}</span>
        <Input v-model:value="editForm.url" />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.intervalLabel')
        }}</span>
        <InputNumber
          v-model:value="editForm.intervalMin"
          :max="10080"
          :min="5"
          class="w-full"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.maxShareLabel')
        }}</span>
        <InputNumber
          v-model:value="editForm.maxSharePct"
          :max="100"
          :min="0"
          class="w-full"
        />
      </label>
      <label v-if="editNeedsAuth" class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          editAuthHeader
            ? $t('dns.blocklist.intel.src.secretLabel', {
                header: editAuthHeader,
              })
            : $t('dns.blocklist.intel.src.secretLabelUrl')
        }}</span>
        <Input.Password
          v-model:value="editForm.secret"
          autocomplete="new-password"
          :placeholder="$t('dns.blocklist.intel.src.secretKeep')"
        />
      </label>
      <label
        v-if="editNeedsAuth"
        class="ipam-form-field--wide flex items-center gap-2"
      >
        <Checkbox v-model:checked="editForm.clearSecret">
          {{ $t('dns.blocklist.intel.src.clearSecret') }}
        </Checkbox>
      </label>
      <div class="ipam-form-hint ipam-form-field--wide">
        {{ $t('dns.blocklist.intel.src.editHint') }}
      </div>
      <div class="ipam-form-hint ipam-form-field--wide">
        {{ $t('dns.blocklist.intel.src.maxShareHint') }}
      </div>
    </div>
  </EditModal>

  <!-- 新增自定义源 -->
  <CreateModal class="w-[640px]">
    <div class="ipam-form-grid">
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.nameLabel')
        }}</span>
        <Input v-model:value="createForm.name" />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.layerLabel')
        }}</span>
        <Select
          v-model:value="createForm.layer"
          :options="[
            { label: $t('dns.blocklist.intel.src.layerHot'), value: 'hot' },
            { label: $t('dns.blocklist.intel.src.layerBroad'), value: 'broad' },
          ]"
        />
      </label>
      <label class="ipam-form-field ipam-form-field--wide">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.urlLabel')
        }}</span>
        <Input
          v-model:value="createForm.url"
          placeholder="https://example.com/domains.txt"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.intervalLabel')
        }}</span>
        <InputNumber
          v-model:value="createForm.intervalMin"
          :max="10080"
          :min="5"
          class="w-full"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{
          $t('dns.blocklist.intel.src.maxShareLabel')
        }}</span>
        <InputNumber
          v-model:value="createForm.maxSharePct"
          :max="100"
          :min="0"
          class="w-full"
        />
      </label>
      <div class="ipam-form-hint ipam-form-field--wide">
        {{ $t('dns.blocklist.intel.src.createHint') }}
      </div>
      <div class="ipam-form-hint ipam-form-field--wide">
        {{ $t('dns.blocklist.intel.src.maxShareHint') }}
      </div>
    </div>
  </CreateModal>

  <!-- 同步记录与历史版本 -->
  <OpsModal class="w-[980px]">
    <div v-if="opsTarget" class="mb-3">
      <div class="ipam-cell-title">
        {{ opsTarget.name }}
        <Tag v-if="opsTarget.builtin" class="ml-1" color="default">
          {{ $t('dns.blocklist.intel.src.builtin') }}
        </Tag>
      </div>
      <div class="ipam-cell-meta ipam-mono">{{ opsTarget.key }}</div>
    </div>
    <Tabs v-model:active-key="opsTab">
      <TabPane key="runs" :tab="$t('dns.blocklist.intel.src.runsTab')">
        <Table
          :columns="runColumns"
          :data-source="runs"
          :loading="opsLoading"
          :locale="{ emptyText: $t('dns.blocklist.intel.src.noRuns') }"
          :pagination="false"
          row-key="id"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'status'">
              <Tag
                class="m-0"
                :color="STATUS_COLOR[record.status] ?? 'default'"
              >
                {{ RUN_STATUS_TEXT[record.status] ?? record.status }}
              </Tag>
              <div
                v-if="record.error"
                class="ipam-cell-meta mt-1"
                :title="record.error"
              >
                {{ record.error }}
              </div>
            </template>
            <template v-else-if="column.key === 'trigger'">
              {{ TRIGGER_TEXT[record.trigger] ?? record.trigger }}
            </template>
            <template v-else-if="column.key === 'startedAt'">
              <span class="ipam-cell-meta">{{ fmtTime(record.startedAt) }}</span>
            </template>
            <template v-else-if="column.key === 'durationMs'">
              {{ fmtDuration(record.durationMs) }}
            </template>
            <template v-else-if="column.key === 'added'">
              <span class="text-green-600">+{{ record.added ?? 0 }}</span>
            </template>
            <template v-else-if="column.key === 'removed'">
              <span class="text-red-500">-{{ record.removed ?? 0 }}</span>
            </template>
            <template v-else-if="column.key === 'revision'">
              {{ record.revision ? `v${record.revision}` : '—' }}
            </template>
          </template>
        </Table>
      </TabPane>
      <TabPane
        key="revisions"
        :tab="$t('dns.blocklist.intel.src.revisionsTab')"
      >
        <Table
          :columns="revisionColumns"
          :data-source="revisions"
          :loading="opsLoading"
          :locale="{ emptyText: $t('dns.blocklist.intel.src.noRevisions') }"
          :pagination="false"
          row-key="revision"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'entries'">
              {{
                $t('dns.common.count', { n: record.entries ?? 0 })
              }}
            </template>
            <template v-else-if="column.key === 'active'">
              <Tag v-if="record.active" class="m-0" color="green">
                {{ $t('dns.blocklist.intel.src.active') }}
              </Tag>
              <span v-else class="ipam-hint">—</span>
            </template>
            <template v-else-if="column.key === 'createdAt'">
              <span class="ipam-cell-meta">{{ fmtTime(record.createdAt) }}</span>
            </template>
            <template v-else-if="column.key === 'op'">
              <span v-if="record.active" class="ipam-hint">—</span>
              <Popconfirm
                v-else
                :cancel-text="$t('dns.common.cancel')"
                :ok-text="$t('dns.blocklist.intel.src.rollback')"
                :title="
                  $t('dns.blocklist.intel.src.rollbackConfirm', {
                    n: record.revision,
                  })
                "
                @confirm="
                  rollbackRevision(record as IntelSourceRevisionRow)
                "
              >
                <Button
                  :loading="rollingBack === record.revision"
                  danger
                  size="small"
                >
                  {{ $t('dns.blocklist.intel.src.rollback') }}
                </Button>
              </Popconfirm>
            </template>
          </template>
        </Table>
      </TabPane>
    </Tabs>
  </OpsModal>
</template>
