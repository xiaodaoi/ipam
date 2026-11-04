<script setup lang="ts">
import type { Upstream } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
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
  createUpstream,
  deleteUpstream,
  listUpstreams,
  updateUpstream,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

const showTotal = (t: number) => $t('dns.common.total', { n: t });

type Proto = 'dot' | 'tcp' | 'udp';
type Health = 'down' | 'unknown' | 'up';

const rows = ref<Upstream[]>([]);
const editingId = ref<string>();
const loading = ref(false);
const form = ref<{
  addr: string;
  enabled: boolean;
  name: string;
  protocol: Proto;
  tlsName: string;
  weight: number;
}>({
  name: '',
  addr: '',
  protocol: 'udp',
  tlsName: '',
  weight: 1,
  enabled: true,
});
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
  loading.value = true;
  try {
    const d = await listUpstreams();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
async function add() {
  if (!form.value.name || !form.value.addr) {
    message.warning($t('dns.upstream.msg.warnNameAddr'));
    return;
  }
  const addrs = form.value.addr
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  if (addrs.length === 0) {
    message.warning($t('dns.upstream.msg.warnAddr'));
    return;
  }
  try {
    if (editingId.value) {
      await updateUpstream(editingId.value, {
        name: form.value.name,
        addrs,
        protocol: form.value.protocol,
        tlsName: form.value.tlsName,
        weight: form.value.weight,
        enabled: form.value.enabled,
      });
      message.success($t('dns.upstream.msg.updated'));
    } else {
      await createUpstream({
        name: form.value.name,
        addrs,
        protocol: form.value.protocol,
        tlsName: form.value.tlsName,
        weight: form.value.weight,
        enabled: form.value.enabled,
      });
      message.success($t('dns.upstream.msg.added'));
    }
    editingId.value = undefined;
    form.value = {
      name: '',
      addr: '',
      protocol: 'udp',
      tlsName: '',
      weight: 1,
      enabled: true,
    };
    formModalApi.close();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.actionFailed'),
    );
  }
  await load();
}
function edit(r: Upstream) {
  editingId.value = r.id;
  formModalApi.setState({
    title: $t('dns.upstream.msg.editTitle'),
    confirmText: $t('dns.upstream.msg.saveChanges'),
  });
  formModalApi.open();
  form.value = {
    name: r.name,
    addr: (r.addrs ?? []).join(','),
    protocol: (r.protocol as Proto) ?? 'udp',
    tlsName: r.tlsName ?? '',
    weight: r.weight ?? 1,
    enabled: r.enabled ?? true,
  };
}
const [FormModal, formModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.upstream.msg.modalTitle'),
  confirmText: $t('dns.upstream.msg.modalConfirmAdd'),
  onConfirm: () => add(),
});
function openAdd() {
  // 不能先 close()（异步回写 isOpen 会把刚打开的弹窗关掉）
  editingId.value = undefined;
  form.value = {
    name: '',
    addr: '',
    protocol: 'udp',
    tlsName: '',
    weight: 1,
    enabled: true,
  };
  formModalApi.setState({
    title: $t('dns.upstream.msg.addTitle'),
    confirmText: $t('dns.upstream.msg.modalConfirmAdd'),
  });
  formModalApi.open();
}
async function toggleEnabled(r: Upstream, checked: boolean) {
  try {
    await updateUpstream(r.id, { enabled: checked });
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
    await deleteUpstream(id);
    message.success($t('dns.common.deletedToast'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.deleteFailed'),
    );
  }
  await load();
}
onMounted(() => {
  void load();
  timer = setInterval(load, 15_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));

// ── 展示映射 ──
const PROTO_TEXT: Record<string, string> = {
  dot: 'DoT',
  tcp: 'TCP',
  udp: 'UDP',
};
const PROTO_COLOR: Record<string, string> = {
  dot: 'purple',
  tcp: 'cyan',
  udp: 'blue',
};
const HEALTH_TEXT = computed<Record<Health, string>>(() => ({
  up: $t('dns.upstream.health.up'),
  down: $t('dns.upstream.health.down'),
  unknown: $t('dns.upstream.health.unknown'),
}));
const HEALTH_COLOR: Record<Health, string> = {
  up: 'green',
  down: 'red',
  unknown: 'default',
};

function healthOf(r: Upstream): Health {
  if (r.health?.up === true) return 'up';
  if (r.health?.up === false) return 'down';
  return 'unknown';
}

function fmtTime(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  if (Number.isNaN(d.getTime()) || d.getFullYear() <= 1970) return '—';
  return d.toLocaleString(undefined, { hour12: false });
}

// ── 指标 ──
const stats = computed(() => {
  const total = rows.value.length;
  const up = rows.value.filter((r) => healthOf(r) === 'up').length;
  const down = rows.value.filter((r) => healthOf(r) === 'down').length;
  const unknown = total - up - down;
  const enabled = rows.value.filter((r) => r.enabled).length;
  const rtts = rows.value
    .map((r) => r.health?.rttMs)
    .filter((v): v is number => typeof v === 'number' && v > 0);
  const avgRtt =
    rtts.length > 0 ? rtts.reduce((a, b) => a + b, 0) / rtts.length : 0;
  const bestRtt = rtts.length > 0 ? Math.min(...rtts) : 0;
  return { avgRtt, bestRtt, down, enabled, total, unknown, up };
});

// ── 筛选 ──
const keyword = ref('');
const protoFilter = ref<'all' | Proto>('all');
const healthFilter = ref<'all' | Health>('all');
const protoOptions = computed(() => [
  { label: $t('dns.upstream.protoOption.all'), value: 'all' },
  { label: 'UDP', value: 'udp' },
  { label: 'TCP', value: 'tcp' },
  { label: 'DoT', value: 'dot' },
]);
const healthOptions = computed(() => [
  { label: $t('dns.common.allStatus'), value: 'all' },
  { label: $t('dns.upstream.health.up'), value: 'up' },
  { label: $t('dns.upstream.health.down'), value: 'down' },
  { label: $t('dns.upstream.health.unknown'), value: 'unknown' },
]);

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (protoFilter.value !== 'all' && r.protocol !== protoFilter.value)
      return false;
    if (healthFilter.value !== 'all' && healthOf(r) !== healthFilter.value)
      return false;
    if (!kw) return true;
    return `${r.name} ${(r.addrs ?? []).join(' ')}`.toLowerCase().includes(kw);
  });
});
function resetFilters() {
  keyword.value = '';
  protoFilter.value = 'all';
  healthFilter.value = 'all';
}

const columns = computed(() => [
  { title: $t('dns.upstream.col.name'), dataIndex: 'name', minWidth: 260 },
  { title: $t('dns.upstream.col.proto'), dataIndex: 'protocol', width: 140 },
  { title: $t('dns.upstream.col.health'), dataIndex: 'health', minWidth: 180 },
  {
    title: $t('dns.upstream.col.lastCheck'),
    dataIndex: 'lastCheck',
    width: 170,
  },
  { title: $t('dns.upstream.col.enabled'), dataIndex: 'enabled', width: 80 },
  { title: $t('dns.common.actions'), key: 'op', width: 140 },
]);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:globe"
      :title="$t('dns.upstream.header.title')"
      :description="$t('dns.upstream.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:activity" class="size-3.5" />
          {{
            $t('dns.upstream.header.meta', {
              up: stats.up,
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
        <Button type="primary" size="small" @click="openAdd()">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.upstream.actions.add')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dns.upstream.stat.total')"
        :value="stats.total"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:server"
        tone="blue"
        :hint="
          $t('dns.upstream.stat.totalHint', {
            enabled: stats.enabled,
            disabled: stats.total - stats.enabled,
          })
        "
      />
      <StatCard
        :label="$t('dns.upstream.stat.up')"
        :value="stats.up"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:circle-check"
        tone="green"
        :progress="stats.total > 0 ? (stats.up / stats.total) * 100 : 0"
        :hint="
          stats.total === 0
            ? $t('dns.upstream.stat.upHintOff')
            : $t('dns.upstream.stat.upHint', {
                pct: Math.round((stats.up / stats.total) * 100),
              })
        "
      />
      <StatCard
        :label="$t('dns.upstream.stat.abnormal')"
        :value="stats.down + stats.unknown"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:triangle-alert"
        :tone="stats.down > 0 ? 'red' : stats.unknown > 0 ? 'amber' : 'green'"
        :hint="
          $t('dns.upstream.stat.abnormalHint', {
            down: stats.down,
            unknown: stats.unknown,
          })
        "
      />
      <StatCard
        :label="$t('dns.upstream.stat.rtt')"
        :value="stats.avgRtt > 0 ? stats.avgRtt.toFixed(1) : '—'"
        :unit="stats.avgRtt > 0 ? 'ms' : ''"
        icon="lucide:timer"
        tone="violet"
        :hint="
          stats.bestRtt > 0
            ? $t('dns.upstream.stat.rttHint', { ms: stats.bestRtt })
            : $t('dns.upstream.stat.rttHintOff')
        "
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-tree" />
          {{ $t('dns.upstream.card.title') }}
          <em class="ipam-card-sub">{{
            $t('dns.upstream.card.sub', {
              filtered: filtered.length,
              total: rows.length,
            })
          }}</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('dns.upstream.card.hint') }}</span>
      </template>

      <div class="ipam-summary mb-3">
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.upstream.summary.up')
          }}</span>
          <span class="ipam-summary__value"
            >{{ stats.up
            }}<small>{{ $t('dns.common.unit.count') }}</small></span
          >
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.upstream.summary.down')
          }}</span>
          <span class="ipam-summary__value"
            >{{ stats.down
            }}<small>{{ $t('dns.common.unit.count') }}</small></span
          >
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.upstream.summary.unknown')
          }}</span>
          <span class="ipam-summary__value"
            >{{ stats.unknown
            }}<small>{{ $t('dns.common.unit.count') }}</small></span
          >
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">{{
            $t('dns.upstream.summary.rtt')
          }}</span>
          <span class="ipam-summary__value"
            >{{ stats.avgRtt > 0 ? stats.avgRtt.toFixed(1) : '—'
            }}<small v-if="stats.avgRtt > 0">ms</small></span
          >
        </div>
      </div>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">{{ $t('dns.common.keyword') }}</span>
          <Input
            v-model:value="keyword"
            allow-clear
            :placeholder="$t('dns.upstream.toolbar.kwPlaceholder')"
            style="width: 220px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.upstream.toolbar.proto')
          }}</span>
          <Select
            v-model:value="protoFilter"
            :options="protoOptions"
            style="width: 130px"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">{{
            $t('dns.upstream.toolbar.health')
          }}</span>
          <Select
            v-model:value="healthFilter"
            :options="healthOptions"
            style="width: 130px"
          />
        </div>
        <Button size="small" @click="resetFilters">
          {{ $t('dns.common.reset') }}
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">{{ $t('dns.upstream.toolbar.hint') }}</span>
      </div>

      <Table
        :data-source="filtered"
        :loading="loading"
        :columns="columns"
        :locale="{
          emptyText:
            keyword || protoFilter !== 'all' || healthFilter !== 'all'
              ? $t('dns.upstream.emptyFiltered')
              : $t('dns.upstream.empty'),
        }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono">{{
                (record.addrs ?? []).join(' · ') || '—'
              }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'protocol'">
            <div class="flex flex-wrap items-center gap-1">
              <Tag
                class="m-0"
                :color="PROTO_COLOR[record.protocol] ?? 'default'"
              >
                {{ PROTO_TEXT[record.protocol] ?? record.protocol }}
              </Tag>
              <span class="ipam-chip" data-tone="muted">{{
                $t('dns.upstream.weight', { n: record.weight ?? 1 })
              }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'health'">
            <div class="flex flex-wrap items-center gap-1">
              <Tag
                class="m-0"
                :color="HEALTH_COLOR[healthOf(record as Upstream)]"
              >
                {{ HEALTH_TEXT[healthOf(record as Upstream)] }}
              </Tag>
              <span
                v-if="
                  record.health?.rttMs !== undefined &&
                  record.health?.rttMs !== null
                "
                class="ipam-chip ipam-mono"
                :data-tone="
                  healthOf(record as Upstream) === 'up' ? 'green' : 'muted'
                "
              >
                {{ record.health.rttMs }} ms
              </span>
              <span
                v-if="(record.health?.consecutiveFails ?? 0) > 0"
                class="ipam-chip"
                data-tone="red"
              >
                {{
                  $t('dns.upstream.fails', {
                    n: record.health.consecutiveFails,
                  })
                }}
              </span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'lastCheck'">
            <span class="ipam-hint">{{
              fmtTime(record.health?.lastCheck)
            }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'enabled'">
            <Switch
              :checked="record.enabled"
              size="small"
              @change="(v: any) => toggleEnabled(record as Upstream, !!v)"
            />
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(record as Upstream)">
                {{ $t('dns.common.edit') }}
              </Button>
              <Popconfirm
                :title="$t('dns.upstream.deleteTitle')"
                :description="$t('dns.upstream.deleteDesc')"
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
    </Card>

    <FormModal class="w-[720px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dns.upstream.form.nameLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="form.name"
            :placeholder="$t('dns.upstream.form.namePlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.upstream.form.protoLabel')
          }}</span>
          <Select
            v-model:value="form.protocol"
            :options="[
              { value: 'udp', label: $t('dns.upstream.form.protoUdp') },
              { value: 'tcp', label: $t('dns.upstream.form.protoTcp') },
              { value: 'dot', label: $t('dns.upstream.form.protoDot') },
            ]"
          />
        </label>
        <label
          v-if="form.protocol === 'dot'"
          class="ipam-form-field ipam-form-field--wide"
        >
          <span class="ipam-form-field__label">{{
            $t('dns.upstream.form.tlsLabel')
          }}</span>
          <Input
            v-model:value="form.tlsName"
            :placeholder="$t('dns.upstream.form.tlsPlaceholder')"
          />
          <span class="ipam-hint">{{ $t('dns.upstream.form.tlsHint') }}</span>
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dns.upstream.form.addrLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="form.addr"
            :placeholder="$t('dns.upstream.form.addrPlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.upstream.form.weightLabel')
          }}</span>
          <InputNumber
            v-model:value="form.weight"
            :min="1"
            :max="100"
            style="width: 100%"
          />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.upstream.form.enabledLabel')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.enabled" size="small" />
            <span class="ipam-hint">{{
              form.enabled
                ? $t('dns.upstream.form.enabledHintOn')
                : $t('dns.upstream.form.enabledHintOff')
            }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.upstream.form.hintWeight')
          }}<code>forward-tls-upstream</code
          >{{ $t('dns.upstream.form.hintTail') }}
        </div>
      </div>
    </FormModal>
  </div>
</template>
