<script setup lang="ts">
import type { DhcpLease4Row, DhcpLease6Row } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

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
  Table,
  Tag,
} from 'ant-design-vue';

import {
  deleteDhcpLease4,
  deleteDhcpLease6,
  listDhcpLeases4,
  listDhcpLeases6,
  updateDhcpLease4,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';
import { filterLeases, formatLeaseRemaining } from '#/utils/lease';

defineOptions({ name: 'DhcpLeases' });

const tab = ref<'v4' | 'v6'>('v4');
const loading = ref(false);
const v4 = ref<DhcpLease4Row[]>([]);
const v6 = ref<DhcpLease6Row[]>([]);
const keyword = ref('');

const extendOpen = ref(false);
const extendTarget = ref<DhcpLease4Row>();
const extendValue = ref(3600);
const extendSaving = ref(false);

const rebindOpen = ref(false);
const rebindTarget = ref<DhcpLease4Row>();
const rebindForm = reactive({ hostname: '', mac: '' });
const rebindSaving = ref(false);

const now = ref(Math.floor(Date.now() / 1000));

const v4Filtered = computed(() => filterLeases(v4.value, keyword.value));
const v6Filtered = computed(() => filterLeases(v6.value, keyword.value));

const declined = computed(
  () => v4.value.filter((r) => (r.state ?? 0) !== 0).length,
);
const pdCount = computed(
  () => v6.value.filter((r) => r.leaseType === 'IA_PD').length,
);

function fmtTime(ts?: number) {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleString();
}

function remaining(row: { cltt?: number; validLifetime?: number }) {
  return formatLeaseRemaining(row, now.value);
}

async function load() {
  loading.value = true;
  now.value = Math.floor(Date.now() / 1000);
  try {
    const [l4, l6] = await Promise.all([
      listDhcpLeases4().catch(() => ({ items: [] })),
      listDhcpLeases6().catch(() => ({ items: [] })),
    ]);
    v4.value = l4.items ?? [];
    v6.value = l6.items ?? [];
  } finally {
    loading.value = false;
  }
}

function openExtend(row: Partial<DhcpLease4Row>) {
  extendTarget.value = row as DhcpLease4Row;
  extendValue.value = row.validLifetime ?? 3600;
  extendOpen.value = true;
}

async function submitExtend() {
  if (!extendTarget.value) return;
  if (!extendValue.value || extendValue.value < 60) {
    message.warning($t('dhcp.leases.messages.lifetimeMin'));
    return;
  }
  extendSaving.value = true;
  try {
    await updateDhcpLease4(extendTarget.value.ipAddress, {
      validLifetime: extendValue.value,
    });
    message.success($t('dhcp.leases.messages.extended'));
    extendOpen.value = false;
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.leases.messages.extendFailed'),
    );
  } finally {
    extendSaving.value = false;
  }
}

function openRebind(row: Partial<DhcpLease4Row>) {
  rebindTarget.value = row as DhcpLease4Row;
  rebindForm.mac = row.hwAddress ?? '';
  rebindForm.hostname = row.hostname ?? '';
  rebindOpen.value = true;
}

async function submitRebind() {
  if (!rebindTarget.value) return;
  if (!/^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$/.test(rebindForm.mac.trim())) {
    message.warning($t('dhcp.leases.messages.macFormat'));
    return;
  }
  rebindSaving.value = true;
  try {
    await updateDhcpLease4(rebindTarget.value.ipAddress, {
      hwAddress: rebindForm.mac.trim().toLowerCase(),
      hostname: rebindForm.hostname.trim(),
    });
    message.success($t('dhcp.leases.messages.rebound'));
    rebindOpen.value = false;
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.leases.messages.rebindFailed'),
    );
  } finally {
    rebindSaving.value = false;
  }
}

async function release4(row: Partial<DhcpLease4Row>) {
  if (!row.ipAddress) return;
  try {
    await deleteDhcpLease4(row.ipAddress);
    message.success($t('dhcp.leases.messages.released', { ip: row.ipAddress }));
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.leases.messages.releaseFailed'),
    );
  }
}

async function release6(row: Partial<DhcpLease6Row>) {
  if (!row.ipAddress || !row.leaseType) return;
  try {
    await deleteDhcpLease6(row.ipAddress, row.leaseType);
    message.success($t('dhcp.leases.messages.released', { ip: row.ipAddress }));
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcp.leases.messages.releaseFailed'),
    );
  }
}

const v4Columns = computed(() => [
  {
    title: $t('dhcp.leases.col.ip'),
    dataIndex: 'ipAddress',
    key: 'ip',
    width: 150,
  },
  { title: 'MAC', dataIndex: 'hwAddress', key: 'mac', width: 160 },
  {
    title: $t('dhcp.leases.col.hostname'),
    dataIndex: 'hostname',
    key: 'hostname',
    ellipsis: true,
  },
  { title: $t('dhcp.leases.col.relay'), key: 'relay', width: 220 },
  {
    title: $t('dhcp.leases.col.subnet'),
    dataIndex: 'subnetId',
    key: 'subnet',
    width: 90,
  },
  {
    title: $t('dhcp.leases.col.cltt'),
    dataIndex: 'cltt',
    key: 'cltt',
    width: 170,
  },
  { title: $t('dhcp.leases.col.remaining'), key: 'remaining', width: 100 },
  { title: $t('dhcp.leases.col.state'), key: 'state', width: 100 },
  { title: $t('dhcp.leases.col.actions'), key: 'actions', width: 210 },
]);

const v6Columns = computed(() => [
  {
    title: $t('dhcp.leases.col.addr'),
    dataIndex: 'ipAddress',
    key: 'ip',
    width: 260,
  },
  {
    title: $t('dhcp.leases.col.type'),
    dataIndex: 'leaseType',
    key: 'type',
    width: 90,
  },
  { title: 'MAC', dataIndex: 'hwAddress', key: 'mac', width: 160 },
  { title: 'DUID', dataIndex: 'duid', key: 'duid', ellipsis: true },
  {
    title: $t('dhcp.leases.col.cltt'),
    dataIndex: 'cltt',
    key: 'cltt',
    width: 170,
  },
  { title: $t('dhcp.leases.col.remaining'), key: 'remaining', width: 100 },
  { title: $t('dhcp.leases.col.actions'), key: 'actions', width: 100 },
]);

const tabOptions = computed(() => [
  { label: $t('dhcp.leases.tabV4', { n: v4.value.length }), value: 'v4' },
  { label: $t('dhcp.leases.tabV6', { n: v6.value.length }), value: 'v6' },
]);

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Leases"
      icon="lucide:clock"
      :title="$t('dhcp.leases.title')"
      :description="$t('dhcp.leases.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:activity" class="size-3.5" />
          IPv4 {{ v4.length }} · IPv6 {{ v6.length }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dhcp.common.refresh')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dhcp.leases.stat.v4')"
        :value="v4.length"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:network"
        tone="green"
        :hint="$t('dhcp.leases.stat.v4Hint')"
      />
      <StatCard
        :label="$t('dhcp.leases.stat.declined')"
        :value="declined"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:alert-triangle"
        :tone="declined > 0 ? 'red' : 'violet'"
        :hint="$t('dhcp.leases.stat.declinedHint')"
      />
      <StatCard
        :label="$t('dhcp.leases.stat.v6')"
        :value="v6.length"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:globe"
        tone="cyan"
        :hint="$t('dhcp.leases.stat.v6Hint')"
      />
      <StatCard
        :label="$t('dhcp.leases.stat.pd')"
        :value="pdCount"
        :unit="$t('dhcp.common.unitRecord')"
        icon="lucide:git-branch"
        tone="amber"
        :hint="$t('dhcp.leases.stat.pdHint')"
      />
    </div>

    <Card>
      <div class="ipam-toolbar ipam-toolbar--plain mb-3">
        <RadioGroup
          v-model:value="tab"
          option-type="button"
          size="small"
          :options="tabOptions"
        />
        <span class="ipam-toolbar__spacer"></span>
        <Input
          v-model:value="keyword"
          allow-clear
          :placeholder="$t('dhcp.leases.searchPlaceholder')"
          style="width: 280px"
        >
          <template #prefix>
            <IconifyIcon icon="lucide:search" class="size-3.5" />
          </template>
        </Input>
      </div>

      <Table
        v-if="tab === 'v4'"
        :columns="v4Columns"
        :data-source="v4Filtered"
        :loading="loading"
        :pagination="{ pageSize: 20, showSizeChanger: false }"
        :scroll="{ x: 1320 }"
        row-key="ipAddress"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'relay'">
            <span v-if="record.circuitId || record.remoteId" class="ipam-mono">
              {{ record.circuitId || '—' }} / {{ record.remoteId || '—' }}
            </span>
            <span v-else class="text-gray-400">—</span>
          </template>
          <template v-else-if="column.key === 'cltt'">
            {{ fmtTime(record.cltt) }}
          </template>
          <template v-else-if="column.key === 'remaining'">
            <Tag
              :color="
                record.cltt + (record.validLifetime ?? 0) > now
                  ? 'green'
                  : 'default'
              "
              class="m-0"
            >
              {{ remaining(record) }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'state'">
            <Tag
              :color="(record.state ?? 0) === 0 ? 'green' : 'red'"
              class="m-0"
            >
              {{
                (record.state ?? 0) === 0
                  ? $t('dhcp.leases.stateOk')
                  : $t('dhcp.leases.stateDeclined')
              }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="flex flex-wrap items-center gap-1">
              <Button size="small" type="link" @click="openExtend(record)">
                {{ $t('dhcp.leases.extend') }}
              </Button>
              <Button size="small" type="link" @click="openRebind(record)">
                {{ $t('dhcp.leases.rebind') }}
              </Button>
              <Popconfirm
                :title="$t('dhcp.leases.confirmRelease')"
                :description="
                  $t('dhcp.leases.confirmReleaseDesc', {
                    mac: record.hwAddress ?? '',
                  })
                "
                :ok-text="$t('dhcp.leases.release')"
                :cancel-text="$t('dhcp.common.cancel')"
                @confirm="release4(record)"
              >
                <Button size="small" type="link" danger>
                  {{ $t('dhcp.leases.release') }}
                </Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>

      <Table
        v-else
        :columns="v6Columns"
        :data-source="v6Filtered"
        :loading="loading"
        :pagination="{ pageSize: 20, showSizeChanger: false }"
        :scroll="{ x: 1000 }"
        row-key="ipAddress"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'type'">
            <Tag
              :color="record.leaseType === 'IA_PD' ? 'orange' : 'blue'"
              class="m-0"
            >
              {{ record.leaseType }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'cltt'">
            {{ fmtTime(record.cltt) }}
          </template>
          <template v-else-if="column.key === 'remaining'">
            {{ remaining(record) }}
          </template>
          <template v-else-if="column.key === 'actions'">
            <Popconfirm
              :title="$t('dhcp.leases.confirmRelease6')"
              :description="
                $t('dhcp.leases.releaseDesc6', {
                  ip: record.ipAddress,
                  type: record.leaseType,
                })
              "
              :ok-text="$t('dhcp.leases.release')"
              :cancel-text="$t('dhcp.common.cancel')"
              @confirm="release6(record)"
            >
              <Button size="small" type="link" danger>
                {{ $t('dhcp.leases.release') }}
              </Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </Card>

    <Modal
      v-model:open="extendOpen"
      :title="$t('dhcp.leases.extendTitle')"
      :ok-text="$t('dhcp.common.save')"
      :cancel-text="$t('dhcp.common.cancel')"
      :confirm-loading="extendSaving"
      @ok="submitExtend"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dhcp.leases.address')
          }}</span>
          <Input :value="extendTarget?.ipAddress" disabled />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.leases.newLifetime') }}
          </span>
          <InputNumber
            v-model:value="extendValue"
            :min="60"
            :max="31536000"
            :step="600"
            style="width: 100%"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dhcp.leases.extendHint') }}
        </div>
      </div>
    </Modal>

    <Modal
      v-model:open="rebindOpen"
      :title="$t('dhcp.leases.rebindTitle')"
      :ok-text="$t('dhcp.common.save')"
      :cancel-text="$t('dhcp.common.cancel')"
      :confirm-loading="rebindSaving"
      @ok="submitRebind"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dhcp.leases.address')
          }}</span>
          <Input :value="rebindTarget?.ipAddress" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcp.leases.newMac')
          }}</span>
          <Input
            v-model:value="rebindForm.mac"
            placeholder="aa:bb:cc:dd:ee:ff"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('dhcp.leases.hostname') }}
          </span>
          <Input
            v-model:value="rebindForm.hostname"
            :placeholder="$t('dhcp.leases.optional')"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ $t('dhcp.leases.rebindHint') }}
        </div>
      </div>
    </Modal>
  </div>
</template>
