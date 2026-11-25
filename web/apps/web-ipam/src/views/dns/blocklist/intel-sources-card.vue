<script setup lang="ts">
import type { AlertPolicy, IntelSourceRow } from '#/api/ipam';

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
  Tag,
} from 'ant-design-vue';

import {
  createIntelSource,
  deleteIntelSource,
  listAlertPolicies,
  listIntelSources,
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
  name: string;
  secret: string;
  url: string;
}>({
  clearSecret: false,
  id: '',
  intervalMin: 360,
  layer: 'hot',
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
  name: string;
  url: string;
}>({ intervalMin: 360, layer: 'hot', name: '', url: '' });
const [CreateModal, createModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.blocklist.intel.src.createTitle'),
  confirmText: $t('dns.common.create'),
  onConfirm: () => saveCreate(),
});
function openCreate() {
  createForm.value = { intervalMin: 360, layer: 'hot', name: '', url: '' };
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
};

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
  { key: 'op', width: 210, title: $t('dns.common.actions') },
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
        </template>
        <template v-else-if="column.key === 'entries'">
          <span class="ipam-chip" data-tone="blue">{{
            $t('dns.common.count', { n: record.entries ?? 0 })
          }}</span>
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
      <div class="ipam-form-hint ipam-form-field--wide">
        {{ $t('dns.blocklist.intel.src.createHint') }}
      </div>
    </div>
  </CreateModal>
</template>
