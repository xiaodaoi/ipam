<script setup lang="ts">
import type { ForwardRule, Upstream } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  message,
  Popconfirm,
  Select,
  Switch,
  Table,
} from 'ant-design-vue';

import {
  createForwardRule,
  deleteForwardRule,
  listForwardRules,
  listUpstreams,
  updateForwardRule,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const showTotal = (t: number) => $t('dns.common.total', { n: t });

const rows = ref<ForwardRule[]>([]);
const upstreams = ref<Upstream[]>([]);
const loading = ref(false);
const editingId = ref<string>();
const form = ref({ domain: '', upstreamId: '', note: '' });

async function load() {
  loading.value = true;
  try {
    const [fr, up] = await Promise.all([listForwardRules(), listUpstreams()]);
    rows.value = fr.items ?? [];
    upstreams.value = up.items ?? [];
  } finally {
    loading.value = false;
  }
}
function upstreamName(id: string): string {
  return upstreams.value.find((u) => u.id === id)?.name ?? id.slice(0, 8);
}
async function add() {
  if (!form.value.domain || !form.value.upstreamId) {
    message.warning($t('dns.forward.msg.warnRequired'));
    return;
  }
  const domain = form.value.domain.endsWith('.')
    ? form.value.domain
    : `${form.value.domain}.`;
  try {
    if (editingId.value) {
      await updateForwardRule(editingId.value, {
        upstreamIds: [form.value.upstreamId],
        note: form.value.note,
      });
      message.success($t('dns.forward.msg.updated'));
    } else {
      await createForwardRule({
        domain,
        upstreamIds: [form.value.upstreamId],
        enabled: true,
        note: form.value.note,
      });
      message.success($t('dns.forward.msg.created'));
    }
    editingId.value = undefined;
    form.value = { domain: '', upstreamId: '', note: '' };
    formModalApi.close();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.actionFailed'),
    );
  }
  await load();
}
const [FormModal, formModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.forward.msg.modalTitle'),
  confirmText: $t('dns.forward.msg.confirmAdd'),
  onConfirm: () => add(),
});
function openAdd() {
  editingId.value = undefined;
  form.value = { domain: '', upstreamId: '', note: '' };
  formModalApi.setState({
    title: $t('dns.forward.msg.addTitle'),
    confirmText: $t('dns.forward.msg.createConfirm'),
  });
  formModalApi.open();
}
function edit(r: ForwardRule) {
  editingId.value = r.id;
  formModalApi.setState({
    title: $t('dns.forward.msg.editTitle'),
    confirmText: $t('dns.forward.msg.saveChanges'),
  });
  formModalApi.open();
  form.value = {
    domain: r.domain,
    upstreamId: (r.upstreamIds ?? [])[0] ?? '',
    note: r.note ?? '',
  };
}
async function toggleEnabled(r: ForwardRule, checked: boolean) {
  try {
    await updateForwardRule(r.id, { enabled: checked });
    message.success(
      checked ? $t('dns.common.enabled') : $t('dns.common.disabled'),
    );
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.actionFailed'),
    );
  }
  await load();
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteForwardRule(id);
    message.success($t('dns.forward.msg.deleted'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.deleteFailed'),
    );
  }
  await load();
}
onMounted(load);

// ── 指标 ──
const stats = computed(() => {
  const total = rows.value.length;
  const enabled = rows.value.filter((r) => r.enabled).length;
  const linked = new Set(rows.value.flatMap((r) => r.upstreamIds ?? [])).size;
  const defaults = rows.value.filter((r) => r.domain === '.').length;
  const linkedIds = new Set(rows.value.flatMap((r) => r.upstreamIds ?? []));
  const linkedOnline = upstreams.value.filter(
    (u) => linkedIds.has(u.id) && u.health?.up === true,
  ).length;
  const linkedDown = upstreams.value.filter(
    (u) => linkedIds.has(u.id) && u.health?.up === false,
  ).length;
  return { defaults, enabled, linked, linkedDown, linkedOnline, total };
});

// ── 筛选 ──
const keyword = ref('');
const statusFilter = ref<'all' | 'disabled' | 'enabled'>('all');
const statusOptions = computed(() => [
  { label: $t('dns.common.allStatus'), value: 'all' },
  { label: $t('dns.common.enabled'), value: 'enabled' },
  { label: $t('dns.common.disabled'), value: 'disabled' },
]);
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (statusFilter.value === 'enabled' && !r.enabled) return false;
    if (statusFilter.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    const names = (r.upstreamIds ?? []).map((id) => upstreamName(id)).join(' ');
    return `${r.domain} ${r.note ?? ''} ${names}`.toLowerCase().includes(kw);
  });
});
function resetFilters() {
  keyword.value = '';
  statusFilter.value = 'all';
}
const upstreamOptions = computed(() =>
  upstreams.value.map((u) => ({
    value: u.id,
    label: `${u.name}${u.health?.up === false ? $t('dns.common.removedSuffix') : ''}`,
  })),
);

const columns = computed(() => [
  { title: $t('dns.forward.col.domain'), dataIndex: 'domain', minWidth: 280 },
  {
    title: $t('dns.forward.col.upstream'),
    dataIndex: 'upstreamIds',
    minWidth: 220,
  },
  { title: $t('dns.forward.col.enabled'), dataIndex: 'enabled', width: 90 },
  { title: $t('dns.common.actions'), key: 'op', width: 140 },
]);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:route"
      :title="$t('dns.forward.header.title')"
      :description="$t('dns.forward.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:route" class="size-3.5" />
          {{
            $t('dns.forward.header.meta', {
              total: stats.total,
              enabled: stats.enabled,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dns.common.refresh')
          }}
        </Button>
        <Button type="primary" size="small" @click="openAdd">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.forward.actions.add')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dns.forward.stat.rules')"
        :value="stats.total"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:route"
        tone="blue"
        :hint="$t('dns.forward.stat.rulesHint')"
      />
      <StatCard
        :label="$t('dns.forward.stat.enabled')"
        :value="stats.enabled"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:toggle-right"
        tone="green"
        :progress="stats.total > 0 ? (stats.enabled / stats.total) * 100 : 0"
        :hint="
          stats.total === 0
            ? $t('dns.forward.stat.enabledHintOff')
            : $t('dns.forward.stat.enabledHint', {
                n: stats.total - stats.enabled,
              })
        "
      />
      <StatCard
        :label="$t('dns.forward.stat.linked')"
        :value="stats.linked"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:server"
        tone="cyan"
        :hint="
          stats.linked > 0
            ? $t('dns.forward.stat.linkedHint', {
                online: stats.linkedOnline,
                down: stats.linkedDown,
              })
            : $t('dns.forward.stat.linkedHintOff')
        "
      />
      <StatCard
        :label="$t('dns.forward.stat.defaults')"
        :value="stats.defaults"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:asterisk"
        tone="violet"
        :hint="
          stats.defaults > 0
            ? $t('dns.forward.stat.defaultsHintOn')
            : $t('dns.forward.stat.defaultsHintOff')
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-tree" />
          {{ $t('dns.forward.card.title') }}
          <em class="ipam-card-sub">{{
            $t('dns.forward.card.sub', {
              filtered: filtered.length,
              total: rows.length,
            })
          }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.forward.card.hint') }}</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.common.keyword') }}</span>
          <Input
            v-model:value="keyword"
            allow-clear
            :placeholder="$t('dns.forward.toolbar.kwPlaceholder')"
            style="width: 260px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.common.status') }}</span>
          <Select
            v-model:value="statusFilter"
            :options="statusOptions"
            style="width: 130px"
          />
        </div>
        <Button size="small" @click="resetFilters">
          {{ $t('dns.common.reset') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dns.forward.toolbar.hint') }}</span>
      </div>

      <Table
        :data-source="filtered"
        :loading="loading"
        :columns="columns"
        :locale="{
          emptyText:
            keyword || statusFilter !== 'all'
              ? $t('dns.forward.emptyFiltered')
              : $t('dns.forward.empty'),
        }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'domain'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title ipam-mono">{{ record.domain }}</span>
              <span class="ipam-cell-meta">{{
                record.note ||
                (record.domain === '.'
                  ? $t('dns.forward.defaultForward')
                  : $t('dns.forward.noNote'))
              }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'upstreamIds'">
            <div
              v-if="(record.upstreamIds ?? []).length > 0"
              class="flex flex-wrap gap-1"
            >
              <span
                v-for="id in record.upstreamIds"
                :key="id"
                class="ipam-chip"
                data-tone="blue"
              >
                {{ upstreamName(id) }}
              </span>
            </div>
            <span v-else class="ipam-hint">{{
              $t('dns.forward.noUpstream')
            }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'enabled'">
            <Switch
              :checked="record.enabled"
              size="small"
              @change="(v: any) => toggleEnabled(record as ForwardRule, !!v)"
            />
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(record as ForwardRule)">
                {{ $t('dns.common.edit') }}
              </Button>
              <Popconfirm
                :title="$t('dns.forward.deleteTitle')"
                :description="$t('dns.forward.deleteDesc')"
                :ok-text="$t('dns.common.delete')"
                :cancel-text="$t('dns.common.cancel')"
                @confirm="remove(record.id)"
              >
                <Button size="small" danger>
                  {{ $t('dns.common.delete') }}
                </Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>

      <div class="ipam-note mt-3">
        <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        <span>{{ $t('dns.forward.note') }}</span>
      </div>
    </Card>

    <FormModal class="w-[640px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dns.forward.form.domainLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="form.domain"
            :placeholder="$t('dns.forward.form.domainPlaceholder')"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dns.forward.form.upstreamLabel') }}<em>*</em></span
          >
          <Select
            v-model:value="form.upstreamId"
            :placeholder="$t('dns.forward.form.upstreamPlaceholder')"
            show-search
            :options="upstreamOptions"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.forward.form.noteLabel')
          }}</span>
          <Input
            v-model:value="form.note"
            :placeholder="$t('dns.forward.form.notePlaceholder')"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.forward.form.hint') }}
        </div>
      </div>
    </FormModal>
  </div>
</template>
