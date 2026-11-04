<script setup lang="ts">
import type { DnsRecord, DnsZone } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Modal,
  Popconfirm,
  RadioGroup,
  Select,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  createDnsRecord,
  createDnsZone,
  deleteDnsRecord,
  deleteDnsZone,
  importDnsHosts,
  listDnsRecords,
  listDnsZones,
  listLinkedRecords,
  updateDnsRecord,
  updateDnsZone,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

type RecType = 'A' | 'AAAA' | 'CNAME' | 'MX' | 'PTR' | 'SRV' | 'TXT';

const showTotal = (t: number) => $t('dns.common.total', { n: t });
const REC_TYPES: RecType[] = ['A', 'AAAA', 'CNAME', 'PTR', 'TXT', 'SRV', 'MX'];

const zones = ref<DnsZone[]>([]);
const zoneId = ref<string>();
const records = ref<DnsRecord[]>([]);
const linked = ref<any[]>([]);
const loading = ref(false);

const activeZone = computed(() =>
  zones.value.find((z) => z.id === zoneId.value),
);

// 完整域名：记录名 + 区域（名称已含区域后缀则只补尾点）
function fqdnOf(name: string): string {
  const z = activeZone.value?.name;
  if (!z) return name;
  if (name.endsWith('.')) return name;
  const root = z.replace(/\.$/, '');
  if (name === root || name.endsWith(`.${root}`)) return `${name}.`;
  return `${name}.${z}`;
}

async function loadZones() {
  const d = await listDnsZones();
  zones.value = d.items ?? [];
  if (!zoneId.value || !zones.value.some((z) => z.id === zoneId.value)) {
    zoneId.value = zones.value[0]?.id;
  }
  if (zoneId.value) await loadRecords();
}

async function loadRecords() {
  if (!zoneId.value) {
    records.value = [];
    linked.value = [];
    return;
  }
  loading.value = true;
  try {
    const [rec, linkedRes] = await Promise.all([
      listDnsRecords(zoneId.value),
      listLinkedRecords(zoneId.value),
    ]);
    records.value = rec.items ?? [];
    linked.value = linkedRes.items ?? [];
  } finally {
    loading.value = false;
  }
}

function selectZone(id: string) {
  zoneId.value = id;
  recTab.value = 'rec';
  void loadRecords();
}

const recTab = ref<'linked' | 'rec'>('rec');

// ── 新增记录表单 ──
const recForm = reactive<{
  name: string;
  rdata: string;
  recType: RecType;
  ttl: number;
}>({
  name: '',
  recType: 'A',
  rdata: '',
  ttl: 300,
});
const [AddRecordModal, addRecordModalApi] = useVbenModal({
  draggable: true,
  title: $t('dns.records.msg.addTitle'),
  confirmText: $t('dns.records.msg.addConfirm'),
  onConfirm: () => addRecord(),
});

function openAddRecord() {
  if (!zoneId.value) {
    message.warning($t('dns.records.msg.warnNoZone'));
    return;
  }
  recForm.name = '';
  recForm.recType = 'A';
  recForm.rdata = '';
  recForm.ttl = 300;
  addRecordModalApi.setState({
    title: $t('dns.records.msg.addTitleNamed', {
      name: activeZone.value?.name ?? '',
    }),
  });
  addRecordModalApi.open();
}

async function addRecord() {
  if (!zoneId.value) return;
  if (!recForm.name || !recForm.rdata) {
    message.warning($t('dns.records.msg.warnNameValue'));
    return;
  }
  try {
    await createDnsRecord(zoneId.value, { ...recForm, enabled: true });
    message.success($t('dns.records.msg.created'));
    addRecordModalApi.close();
    recForm.name = '';
    recForm.rdata = '';
    await loadRecords();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.addFailed'),
    );
  }
}

// ── 编辑记录 ──
const editRecord = ref<DnsRecord>();
const recordEdit = reactive<{
  enabled: boolean;
  name: string;
  rdata: string;
  recType: RecType;
  ttl: number;
}>({
  name: '',
  recType: 'A',
  rdata: '',
  ttl: 300,
  enabled: true,
});
const [RecordModal, recordModalApi] = useVbenModal({
  draggable: true,
  confirmText: $t('dns.common.save'),
  onConfirm: () => confirmEditRecord(),
});

function openEditRecord(r: DnsRecord) {
  editRecord.value = r;
  Object.assign(recordEdit, {
    name: r.name,
    recType: r.recType,
    rdata: r.rdata,
    ttl: r.ttl,
    enabled: r.enabled,
  });
  recordModalApi.setState({
    title: $t('dns.records.msg.editTitleNamed', { name: r.name }),
  });
  recordModalApi.open();
}

async function confirmEditRecord() {
  const r = editRecord.value;
  if (!r || !zoneId.value) return;
  if (!recordEdit.name || !recordEdit.rdata) {
    message.warning($t('dns.records.msg.warnNameValueRequired'));
    return;
  }
  try {
    await updateDnsRecord(zoneId.value, r.id, { ...recordEdit });
    message.success($t('dns.records.msg.updated'));
    recordModalApi.close();
    await loadRecords();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.updateFailed'),
    );
  }
}

async function toggleRecord(r: DnsRecord, enabled: boolean) {
  if (!zoneId.value) return;
  try {
    await updateDnsRecord(zoneId.value, r.id, { enabled });
    await loadRecords();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.actionFailed'),
    );
  }
}

function removeRecord(r: DnsRecord) {
  Modal.confirm({
    title: $t('dns.records.msg.deleteTitle', { name: r.name }),
    content: $t('dns.records.msg.deleteContent', {
      zone: activeZone.value?.name ?? '',
      name: r.name,
      type: r.recType,
    }),
    okText: $t('dns.common.delete'),
    okType: 'danger',
    onOk: async () => {
      if (!zoneId.value) return;
      try {
        await deleteDnsRecord(zoneId.value, r.id);
        message.success($t('dns.records.msg.deleted'));
        await loadRecords();
      } catch (error) {
        message.error(
          error instanceof Error
            ? error.message
            : $t('dns.common.deleteFailed'),
        );
      }
    },
  });
}

// ── 新建/编辑 zone ──
const zoneForm = reactive<{
  kind: 'auth' | 'local' | 'secondary';
  name: string;
  primary: string;
}>({
  name: '',
  kind: 'auth',
  primary: '',
});
const editingZoneId = ref<string>();
const [ZoneModal, zoneModalApi] = useVbenModal({
  draggable: true,
  confirmText: $t('dns.common.save'),
  onConfirm: () => confirmZone(),
});

function openCreateZone() {
  editingZoneId.value = undefined;
  zoneForm.name = '';
  zoneForm.kind = 'auth';
  zoneForm.primary = '';
  zoneModalApi.setState({
    title: $t('dns.records.msg.zoneCreateTitle'),
    confirmText: $t('dns.records.msg.zoneCreateConfirm'),
  });
  zoneModalApi.open();
}

function openEditZone(z: DnsZone) {
  editingZoneId.value = z.id;
  zoneForm.name = z.name.replace(/\.$/, '');
  zoneForm.kind = z.kind as 'auth' | 'local' | 'secondary';
  zoneForm.primary = z.primary ?? '';
  zoneModalApi.setState({
    title: $t('dns.records.msg.zoneEditTitle', { name: z.name }),
    confirmText: $t('dns.records.msg.saveChanges'),
  });
  zoneModalApi.open();
}

async function confirmZone() {
  const name = zoneForm.name.trim();
  if (!name) return;
  const fqdn = name.endsWith('.') ? name : `${name}.`;
  try {
    if (editingZoneId.value) {
      await updateDnsZone(editingZoneId.value, {
        name: fqdn,
        kind: zoneForm.kind,
        primary: zoneForm.kind === 'secondary' ? zoneForm.primary : '',
      });
      message.success($t('dns.records.msg.zoneUpdated'));
    } else {
      await createDnsZone({
        name: fqdn,
        kind: zoneForm.kind,
        primary: zoneForm.kind === 'secondary' ? zoneForm.primary : undefined,
      });
      message.success($t('dns.records.msg.zoneCreated'));
    }
    zoneModalApi.close();
    await loadZones();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.saveFailed'),
    );
  }
}

async function toggleZone(z: DnsZone, enabled: boolean) {
  try {
    await updateDnsZone(z.id, { enabled });
    await loadZones();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('dns.common.actionFailed'),
    );
  }
}

function removeZone(z: DnsZone) {
  Modal.confirm({
    title: $t('dns.records.msg.zoneDeleteTitle', { name: z.name }),
    content: $t('dns.records.msg.zoneDeleteContent'),
    okText: $t('dns.common.delete'),
    okType: 'danger',
    onOk: async () => {
      try {
        await deleteDnsZone(z.id);
        if (zoneId.value === z.id) zoneId.value = undefined;
        message.success($t('dns.records.msg.zoneDeleted'));
        await loadZones();
      } catch (error) {
        message.error(
          error instanceof Error
            ? error.message
            : $t('dns.common.deleteFailed'),
        );
      }
    },
  });
}

onMounted(loadZones);

// ── 指标 ──
const stats = computed(() => {
  const zoneTotal = zones.value.length;
  const zoneEnabled = zones.value.filter((z) => z.enabled).length;
  const disabled = records.value.filter((r) => !r.enabled).length;
  const typeCount = new Map<string, number>();
  for (const r of records.value)
    typeCount.set(r.recType, (typeCount.get(r.recType) ?? 0) + 1);
  return {
    disabled,
    enabledRecords: records.value.length - disabled,
    linked: linked.value.length,
    records: records.value.length,
    typeCount,
    zoneEnabled,
    zoneTotal,
  };
});

// ── 区域筛选 ──
const zoneKeyword = ref('');
const zoneStatus = ref<'all' | 'disabled' | 'enabled'>('all');
const filteredZones = computed(() => {
  const kw = zoneKeyword.value.trim().toLowerCase();
  return zones.value.filter((z) => {
    if (zoneStatus.value === 'enabled' && !z.enabled) return false;
    if (zoneStatus.value === 'disabled' && z.enabled) return false;
    return !kw || z.name.toLowerCase().includes(kw);
  });
});

// ── 记录筛选 ──
const recKeyword = ref('');
const hostsOpen = ref(false);
const hostsContent = ref('');
const importing = ref(false);

function openHostsImport() {
  if (!zoneId.value) {
    message.warning($t('dns.records.msg.warnZone'));
    return;
  }
  hostsContent.value = '';
  hostsOpen.value = true;
}

async function submitHostsImport() {
  if (!zoneId.value || !hostsContent.value.trim()) {
    message.warning($t('dns.records.hosts.needContent'));
    return;
  }
  importing.value = true;
  try {
    const r = await importDnsHosts(zoneId.value, hostsContent.value);
    message.success(
      $t('dns.records.hosts.importOk', {
        created: r.created,
        skipped: r.skipped,
      }),
    );
    if (r.errors?.length)
      message.warning(r.errors.slice(0, 3).join($t('dns.common.listSep')));
    hostsOpen.value = false;
    await loadRecords();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dns.records.hosts.importFailed'),
    );
  } finally {
    importing.value = false;
  }
}

const recTypeFilter = ref<'all' | RecType>('all');
const recStatus = ref<'all' | 'disabled' | 'enabled'>('all');
const typeOptions = computed(() => [
  { label: $t('dns.common.allTypes'), value: 'all' },
  ...REC_TYPES.map((t) => ({ label: t, value: t })),
]);
const recFiltered = computed(() => {
  const kw = recKeyword.value.trim().toLowerCase();
  return records.value.filter((r) => {
    if (recTypeFilter.value !== 'all' && r.recType !== recTypeFilter.value)
      return false;
    if (recStatus.value === 'enabled' && !r.enabled) return false;
    if (recStatus.value === 'disabled' && r.enabled) return false;
    if (!kw) return true;
    return `${r.name} ${fqdnOf(r.name)} ${r.rdata}`.toLowerCase().includes(kw);
  });
});
const linkedKeyword = ref('');
const linkedFiltered = computed(() => {
  const kw = linkedKeyword.value.trim().toLowerCase();
  if (!kw) return linked.value;
  return linked.value.filter((r) =>
    `${r.name ?? ''} ${r.rdata ?? ''} ${r.mac ?? ''}`
      .toLowerCase()
      .includes(kw),
  );
});
function resetRecFilters() {
  recKeyword.value = '';
  recTypeFilter.value = 'all';
  recStatus.value = 'all';
}

const recColumns = computed(() => [
  {
    title: $t('dns.records.recordCard.colName'),
    dataIndex: 'name',
    minWidth: 260,
  },
  {
    title: $t('dns.records.recordCard.colType'),
    dataIndex: 'recType',
    width: 80,
  },
  { title: $t('dns.records.recordCard.colTtl'), dataIndex: 'ttl', width: 90 },
  {
    title: $t('dns.records.recordCard.colValue'),
    dataIndex: 'rdata',
    minWidth: 200,
  },
  {
    title: $t('dns.records.recordCard.colEnabled'),
    dataIndex: 'enabled',
    width: 80,
  },
  { title: $t('dns.common.actions'), key: 'op', width: 140 },
]);
const linkedColumns = computed(() => [
  { title: $t('dns.records.linked.colName'), dataIndex: 'name', minWidth: 200 },
  {
    title: $t('dns.records.recordCard.colType'),
    dataIndex: 'recType',
    width: 90,
  },
  {
    title: $t('dns.records.linked.colValue'),
    dataIndex: 'rdata',
    minWidth: 200,
  },
  { title: $t('dns.records.linked.colMac'), dataIndex: 'mac', minWidth: 160 },
]);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:book-open"
      :title="$t('dns.records.header.title')"
      :description="$t('dns.records.header.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:layers" class="size-3.5" />
          {{
            $t('dns.records.header.meta', {
              zones: stats.zoneTotal,
              records: stats.records,
            })
          }}
        </span>
        <Button size="small" :loading="loading" @click="loadZones()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dns.common.refresh')
          }}
        </Button>
        <Button size="small" @click="openCreateZone">
          <IconifyIcon icon="lucide:folder-plus" class="mr-1" />{{
            $t('dns.records.actions.newZone')
          }}
        </Button>
        <Button size="small" @click="openHostsImport">
          <IconifyIcon icon="lucide:upload" class="mr-1" />{{
            $t('dns.records.actions.importHosts')
          }}
        </Button>
        <Button type="primary" size="small" @click="openAddRecord">
          <IconifyIcon icon="lucide:plus" class="mr-1" />{{
            $t('dns.records.actions.addRecord')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dns.records.stat.zones')"
        :value="stats.zoneTotal"
        :unit="$t('dns.common.unit.count')"
        icon="lucide:layers"
        tone="blue"
        :hint="
          $t('dns.records.stat.zoneHint', {
            enabled: stats.zoneEnabled,
            disabled: stats.zoneTotal - stats.zoneEnabled,
          })
        "
      />
      <StatCard
        :label="$t('dns.records.stat.records')"
        :value="stats.records"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:book-open"
        tone="cyan"
        :hint="
          activeZone
            ? $t('dns.records.stat.recordsHint', {
                zone: activeZone.name,
                enabled: stats.enabledRecords,
              })
            : $t('dns.records.stat.selectZone')
        "
      />
      <StatCard
        :label="$t('dns.records.stat.linked')"
        :value="stats.linked"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:git-compare-arrows"
        tone="violet"
        :hint="
          stats.linked > 0
            ? $t('dns.records.stat.linkedHintOn')
            : $t('dns.records.stat.linkedHintOff')
        "
      />
      <StatCard
        :label="$t('dns.records.stat.disabled')"
        :value="stats.disabled"
        :unit="$t('dns.common.unit.item')"
        icon="lucide:eye-off"
        :tone="stats.disabled > 0 ? 'amber' : 'green'"
        :hint="
          stats.records > 0
            ? $t('dns.records.stat.disabledHint', {
                pct: Math.round((stats.enabledRecords / stats.records) * 100),
              })
            : $t('dns.records.stat.disabledHintOff')
        "
      />
    </div>

    <div class="ipam-record-layout">
      <!-- 左侧 zone 列表 -->
      <Card class="ipam-side-card ipam-side-card--wide">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:layers" />
            {{ $t('dns.records.zoneCard.title') }}
            <em class="ipam-card-sub"
              >{{ filteredZones.length }} / {{ zones.length }}</em
            >
          </span>
        </template>

        <div class="mb-2 flex flex-col gap-2">
          <Input
            v-model:value="zoneKeyword"
            allow-clear
            :placeholder="$t('dns.records.zoneCard.searchPlaceholder')"
            size="small"
          />
          <Select
            v-model:value="zoneStatus"
            size="small"
            :options="[
              { label: $t('dns.common.allStatus'), value: 'all' },
              {
                label: $t('dns.records.zoneCard.statusEnabled'),
                value: 'enabled',
              },
              { label: $t('dns.common.disabled'), value: 'disabled' },
            ]"
          />
        </div>

        <div
          v-if="filteredZones.length === 0"
          class="ipam-empty ipam-empty--compact"
        >
          <div class="ipam-empty__title">
            {{
              zones.length === 0
                ? $t('dns.records.zoneCard.emptyTitleNoZone')
                : $t('dns.records.zoneCard.emptyTitleNoMatch')
            }}
          </div>
          <div class="ipam-empty__desc">
            {{
              zones.length === 0
                ? $t('dns.records.zoneCard.emptyDescNoZone')
                : $t('dns.records.zoneCard.emptyDescNoMatch')
            }}
          </div>
        </div>

        <div
          v-for="z in filteredZones"
          :key="z.id"
          class="ipam-zone-item"
          :class="zoneId === z.id ? 'ipam-zone-item--active' : ''"
          @click="selectZone(z.id)"
        >
          <div class="flex items-center justify-between gap-1">
            <span class="truncate text-sm font-medium" :title="z.name">{{
              z.name
            }}</span>
            <Tag :color="z.kind === 'auth' ? 'blue' : 'purple'" class="m-0">
              {{ z.kind }}
            </Tag>
          </div>
          <div class="mt-1 flex items-center justify-between">
            <span class="ipam-hint">{{
              z.enabled
                ? $t('dns.records.zoneCard.statusEnabled')
                : $t('dns.common.disabled')
            }}</span>
            <div class="flex items-center gap-1" @click.stop>
              <Switch
                :checked="z.enabled"
                size="small"
                @change="(v: any) => toggleZone(z as DnsZone, !!v)"
              />
              <Button size="small" type="text" @click="openEditZone(z)">
                {{ $t('dns.common.edit') }}
              </Button>
              <Button size="small" type="text" danger @click="removeZone(z)">
                {{ $t('dns.common.delete') }}
              </Button>
            </div>
          </div>
        </div>
      </Card>

      <!-- 右侧记录 -->
      <Card class="min-w-0 flex-1">
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:list-tree" />
            {{ $t('dns.records.recordCard.title') }}
            <em class="ipam-card-sub">{{
              activeZone ? activeZone.name : $t('dns.records.recordCard.noZone')
            }}</em>
          </span>
        </template>

        <div v-if="activeZone">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
            <RadioGroup
              v-model:value="recTab"
              option-type="button"
              size="small"
              :options="[
                {
                  label: $t('dns.records.recordCard.tabRec', {
                    n: records.length,
                  }),
                  value: 'rec',
                },
                {
                  label: $t('dns.records.recordCard.tabLinked', {
                    n: linked.length,
                  }),
                  value: 'linked',
                },
              ]"
            />
            <div class="flex flex-wrap items-center gap-2">
              <span class="ipam-chip" data-tone="muted"
                >A {{ stats.typeCount.get('A') ?? 0 }}</span
              >
              <span class="ipam-chip" data-tone="muted"
                >AAAA {{ stats.typeCount.get('AAAA') ?? 0 }}</span
              >
              <span class="ipam-chip" data-tone="muted"
                >CNAME {{ stats.typeCount.get('CNAME') ?? 0 }}</span
              >
              <span class="ipam-chip" data-tone="muted"
                >PTR {{ stats.typeCount.get('PTR') ?? 0 }}</span
              >
              <span class="ipam-chip" data-tone="muted"
                >TXT {{ stats.typeCount.get('TXT') ?? 0 }}</span
              >
              <span class="ipam-chip" data-tone="muted"
                >SRV {{ stats.typeCount.get('SRV') ?? 0 }}</span
              >
              <span class="ipam-chip" data-tone="muted"
                >MX {{ stats.typeCount.get('MX') ?? 0 }}</span
              >
            </div>
          </div>

          <template v-if="recTab === 'rec'">
            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">{{
                  $t('dns.common.keyword')
                }}</span>
                <Input
                  v-model:value="recKeyword"
                  allow-clear
                  :placeholder="$t('dns.records.recordCard.kwPlaceholder')"
                  style="width: 240px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">{{
                  $t('dns.common.type')
                }}</span>
                <Select
                  v-model:value="recTypeFilter"
                  :options="typeOptions"
                  style="width: 120px"
                />
              </div>
              <div class="ipam-field">
                <span class="ipam-field__label">{{
                  $t('dns.common.status')
                }}</span>
                <Select
                  v-model:value="recStatus"
                  :options="[
                    { label: $t('dns.common.allStatus'), value: 'all' },
                    {
                      label: $t('dns.records.recordCard.statusEnabled'),
                      value: 'enabled',
                    },
                    {
                      label: $t('dns.records.recordCard.statusDisabled'),
                      value: 'disabled',
                    },
                  ]"
                  style="width: 120px"
                />
              </div>
              <Button size="small" @click="resetRecFilters">
                {{ $t('dns.common.reset') }}
              </Button>
            </div>

            <Table
              :data-source="recFiltered"
              :loading="loading"
              :columns="recColumns"
              :locale="{
                emptyText:
                  records.length === 0
                    ? $t('dns.records.recordCard.emptyNoRecords')
                    : $t('dns.records.recordCard.emptyFiltered'),
              }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              row-key="id"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'name'">
                  <div class="ipam-cell-stack">
                    <span class="ipam-cell-title ipam-mono">{{
                      record.name
                    }}</span>
                    <span class="ipam-cell-meta ipam-mono">{{
                      fqdnOf(record.name)
                    }}</span>
                  </div>
                </template>
                <template v-else-if="column.dataIndex === 'recType'">
                  <Tag
                    class="m-0"
                    :color="
                      record.recType === 'AAAA'
                        ? 'cyan'
                        : record.recType === 'CNAME'
                          ? 'purple'
                          : record.recType === 'TXT'
                            ? 'green'
                            : record.recType === 'SRV' ||
                                record.recType === 'MX'
                              ? 'orange'
                              : 'blue'
                    "
                  >
                    {{ record.recType }}
                  </Tag>
                </template>
                <template v-else-if="column.dataIndex === 'ttl'">
                  <span class="ipam-hint">{{ record.ttl }}s</span>
                </template>
                <template v-else-if="column.dataIndex === 'rdata'">
                  <span class="ipam-mono text-xs">{{ record.rdata }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'enabled'">
                  <Switch
                    :checked="record.enabled"
                    size="small"
                    @change="(v: any) => toggleRecord(record as DnsRecord, !!v)"
                  />
                </template>
                <template v-else-if="column.key === 'op'">
                  <div class="flex items-center gap-1">
                    <Button
                      size="small"
                      @click="openEditRecord(record as DnsRecord)"
                    >
                      {{ $t('dns.common.edit') }}
                    </Button>
                    <Popconfirm
                      :title="$t('dns.records.recordCard.deleteTitle')"
                      :description="$t('dns.records.recordCard.deleteDesc')"
                      :ok-text="$t('dns.common.delete')"
                      :cancel-text="$t('dns.common.cancel')"
                      @confirm="removeRecord(record as DnsRecord)"
                    >
                      <Button size="small" danger>
                        {{ $t('dns.common.delete') }}
                      </Button>
                    </Popconfirm>
                  </div>
                </template>
              </template>
            </Table>
          </template>

          <template v-else>
            <div class="ipam-toolbar mb-3">
              <div class="ipam-field">
                <span class="ipam-field__label">{{
                  $t('dns.common.keyword')
                }}</span>
                <Input
                  v-model:value="linkedKeyword"
                  allow-clear
                  :placeholder="$t('dns.records.linked.kwPlaceholder')"
                  style="width: 240px"
                />
              </div>
              <div class="ipam-toolbar__spacer"></div>
              <span class="ipam-hint">{{ $t('dns.records.linked.hint') }}</span>
            </div>

            <div class="ipam-note mb-3">
              <IconifyIcon
                icon="lucide:info"
                class="mt-0.5 size-3.5 shrink-0"
              />
              <span>{{ $t('dns.records.linked.note') }}</span>
            </div>

            <Table
              :data-source="linkedFiltered"
              :loading="loading"
              :columns="linkedColumns"
              :locale="{
                emptyText:
                  linked.length === 0
                    ? $t('dns.records.linked.emptyNoRecords')
                    : $t('dns.records.linked.emptyFiltered'),
              }"
              :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
              row-key="name"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'name'">
                  <span class="ipam-mono text-xs">{{ record.name }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'recType'">
                  <Tag
                    class="m-0"
                    :color="record.recType === 'AAAA' ? 'cyan' : 'blue'"
                  >
                    {{ record.recType }}
                  </Tag>
                </template>
                <template v-else-if="column.dataIndex === 'rdata'">
                  <span class="ipam-mono text-xs">{{ record.rdata }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'mac'">
                  <span class="ipam-chip ipam-mono" data-tone="muted">{{
                    record.mac || '—'
                  }}</span>
                </template>
              </template>
            </Table>
          </template>
        </div>
        <div v-else class="ipam-empty">
          <div class="ipam-empty__title">
            {{ $t('dns.records.emptyNoZone.title') }}
          </div>
          <div class="ipam-empty__desc">
            {{ $t('dns.records.emptyNoZone.desc') }}
          </div>
        </div>
      </Card>
    </div>

    <!-- 新增记录弹窗 -->
    <AddRecordModal class="w-[680px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.addModal.nameLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="recForm.name"
            :placeholder="$t('dns.records.addModal.namePlaceholder')"
          />
          <span class="ipam-hint">{{
            $t('dns.records.addModal.nameHint')
          }}</span>
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.addModal.typeLabel') }}<em>*</em></span
          >
          <Select
            v-model:value="recForm.recType"
            :options="REC_TYPES.map((v) => ({ value: v, label: v }))"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.addModal.valueLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="recForm.rdata"
            :placeholder="$t('dns.records.addModal.valuePlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.records.addModal.ttlLabel')
          }}</span>
          <InputNumber
            v-model:value="recForm.ttl"
            :min="1"
            :max="86400"
            style="width: 100%"
          />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.records.addModal.fqdnLabel')
          }}</span>
          <div class="flex h-8 items-center">
            <span class="ipam-chip ipam-mono" data-tone="blue">{{
              fqdnOf(recForm.name || $t('dns.records.addModal.nameFallback'))
            }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.records.addModal.hint') }}
        </div>
      </div>
    </AddRecordModal>

    <!-- 记录编辑弹窗 -->
    <RecordModal class="w-[680px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.editModal.nameLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="recordEdit.name"
            :placeholder="$t('dns.records.addModal.namePlaceholder')"
          />
          <span class="ipam-hint">{{
            $t('dns.records.editModal.nameHint')
          }}</span>
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.common.type')
          }}</span>
          <Select
            v-model:value="recordEdit.recType"
            :options="REC_TYPES.map((v) => ({ value: v, label: v }))"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.addModal.valueLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="recordEdit.rdata"
            :placeholder="$t('dns.records.editModal.valuePlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.records.addModal.ttlLabel')
          }}</span>
          <InputNumber
            v-model:value="recordEdit.ttl"
            :min="1"
            :max="86400"
            style="width: 100%"
          />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.records.editModal.enabledLabel')
          }}</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="recordEdit.enabled" size="small" />
            <span class="ipam-hint">{{
              recordEdit.enabled
                ? $t('dns.records.editModal.enabledHintOn')
                : $t('dns.records.editModal.enabledHintOff')
            }}</span>
          </div>
        </div>
      </div>
    </RecordModal>

    <!-- zone 弹窗 -->
    <ZoneModal class="w-[600px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.zoneModal.domainLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="zoneForm.name"
            :placeholder="$t('dns.records.zoneModal.domainPlaceholder')"
          />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dns.records.zoneModal.kindLabel')
          }}</span>
          <Select
            v-model:value="zoneForm.kind"
            :options="[
              { value: 'auth', label: $t('dns.records.zoneModal.kindAuth') },
              { value: 'local', label: $t('dns.records.zoneModal.kindLocal') },
              {
                value: 'secondary',
                label: $t('dns.records.zoneModal.kindSecondary'),
              },
            ]"
          />
        </label>
        <label
          v-if="zoneForm.kind === 'secondary'"
          class="ipam-form-field ipam-form-field--wide"
        >
          <span class="ipam-form-field__label"
            >{{ $t('dns.records.zoneModal.primaryLabel') }}<em>*</em></span
          >
          <Input
            v-model:value="zoneForm.primary"
            :placeholder="$t('dns.records.zoneModal.primaryPlaceholder')"
          />
        </label>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.records.zoneModal.hint') }}
        </div>
      </div>
    </ZoneModal>

    <Modal
      v-model:open="hostsOpen"
      :title="$t('dns.records.hosts.title')"
      :ok-text="$t('dns.records.hosts.ok')"
      :cancel-text="$t('dns.common.cancel')"
      :confirm-loading="importing"
      @ok="submitHostsImport"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dns.records.hosts.contentLabel')
          }}</span>
          <Input.TextArea
            v-model:value="hostsContent"
            :rows="10"
            class="ipam-mono"
            :placeholder="$t('dns.records.hosts.contentPlaceholder')"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dns.records.hosts.hint') }}
        </div>
      </div>
    </Modal>
  </div>
</template>
