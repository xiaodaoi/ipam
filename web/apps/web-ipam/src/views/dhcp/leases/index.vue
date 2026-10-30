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

const v4Filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return v4.value;
  return v4.value.filter((r) =>
    [r.ipAddress, r.hwAddress, r.hostname, String(r.subnetId ?? '')]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(kw)),
  );
});

const v6Filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return v6.value;
  return v6.value.filter((r) =>
    [r.ipAddress, r.hwAddress, r.duid, r.leaseType]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(kw)),
  );
});

const declined = computed(() => v4.value.filter((r) => (r.state ?? 0) !== 0).length);
const pdCount = computed(() => v6.value.filter((r) => r.leaseType === 'IA_PD').length);

function fmtTime(ts?: number) {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleString();
}

function remaining(row: { cltt?: number; validLifetime?: number }) {
  if (!row.cltt || !row.validLifetime) return '—';
  const left = row.cltt + row.validLifetime - now.value;
  if (left <= 0) return '已过期';
  if (left < 3600) return `${Math.ceil(left / 60)} 分钟`;
  if (left < 24 * 3600) return `${(left / 3600).toFixed(1)} 小时`;
  return `${(left / (24 * 3600)).toFixed(1)} 天`;
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
    message.warning('租约时长至少 60 秒');
    return;
  }
  extendSaving.value = true;
  try {
    await updateDhcpLease4(extendTarget.value.ipAddress, { validLifetime: extendValue.value });
    message.success('租约已延期');
    extendOpen.value = false;
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '延期失败');
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
    message.warning('MAC 格式应为 aa:bb:cc:dd:ee:ff');
    return;
  }
  rebindSaving.value = true;
  try {
    await updateDhcpLease4(rebindTarget.value.ipAddress, {
      hwAddress: rebindForm.mac.trim().toLowerCase(),
      hostname: rebindForm.hostname.trim(),
    });
    message.success('租约已改绑');
    rebindOpen.value = false;
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '改绑失败');
  } finally {
    rebindSaving.value = false;
  }
}

async function release4(row: Partial<DhcpLease4Row>) {
  if (!row.ipAddress) return;
  try {
    await deleteDhcpLease4(row.ipAddress);
    message.success(`已释放 ${row.ipAddress}`);
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '释放失败');
  }
}

async function release6(row: Partial<DhcpLease6Row>) {
  if (!row.ipAddress || !row.leaseType) return;
  try {
    await deleteDhcpLease6(row.ipAddress, row.leaseType);
    message.success(`已释放 ${row.ipAddress}`);
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '释放失败');
  }
}

const v4Columns = [
  { title: 'IP 地址', dataIndex: 'ipAddress', key: 'ip', width: 150 },
  { title: 'MAC', dataIndex: 'hwAddress', key: 'mac', width: 160 },
  { title: '主机名', dataIndex: 'hostname', key: 'hostname', ellipsis: true },
  { title: 'Kea 子网', dataIndex: 'subnetId', key: 'subnet', width: 90 },
  { title: '最近事务', dataIndex: 'cltt', key: 'cltt', width: 170 },
  { title: '剩余', key: 'remaining', width: 100 },
  { title: '状态', key: 'state', width: 100 },
  { title: '操作', key: 'actions', width: 210 },
];

const v6Columns = [
  { title: '地址 / 前缀', dataIndex: 'ipAddress', key: 'ip', width: 260 },
  { title: '类型', dataIndex: 'leaseType', key: 'type', width: 90 },
  { title: 'MAC', dataIndex: 'hwAddress', key: 'mac', width: 160 },
  { title: 'DUID', dataIndex: 'duid', key: 'duid', ellipsis: true },
  { title: '最近事务', dataIndex: 'cltt', key: 'cltt', width: 170 },
  { title: '剩余', key: 'remaining', width: 100 },
  { title: '操作', key: 'actions', width: 100 },
];

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DHCP Leases"
      icon="lucide:clock"
      title="租约管理"
      description="实时查看 Kea 租约，支持延期、改绑与释放；操作直接作用于 DHCP 服务，无需重启。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:activity" class="size-3.5" />
          IPv4 {{ v4.length }} · IPv6 {{ v6.length }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard label="IPv4 租约" :value="v4.length" unit="条" icon="lucide:network" tone="green" hint="Kea lease4-get-all 实时投影" />
      <StatCard label="v4 冲突/保留" :value="declined" unit="条" icon="lucide:alert-triangle" :tone="declined > 0 ? 'red' : 'violet'" hint="state != 0（declined）" />
      <StatCard label="IPv6 租约" :value="v6.length" unit="条" icon="lucide:globe" tone="cyan" hint="IA_NA + IA_PD" />
      <StatCard label="前缀委派" :value="pdCount" unit="条" icon="lucide:git-branch" tone="amber" hint="IA_PD 租约" />
    </div>

    <Card>
      <div class="ipam-toolbar ipam-toolbar--plain mb-3">
        <RadioGroup
          v-model:value="tab"
          option-type="button"
          size="small"
          :options="[
            { label: `IPv4（${v4.length}）`, value: 'v4' },
            { label: `IPv6（${v6.length}）`, value: 'v6' },
          ]"
        />
        <span class="ipam-toolbar__spacer"></span>
        <Input
          v-model:value="keyword"
          allow-clear
          placeholder="搜索 IP / MAC / 主机名 / DUID"
          style="width: 280px"
        >
          <template #prefix><IconifyIcon icon="lucide:search" class="size-3.5" /></template>
        </Input>
      </div>

      <Table
        v-if="tab === 'v4'"
        :columns="v4Columns"
        :data-source="v4Filtered"
        :loading="loading"
        :pagination="{ pageSize: 20, showSizeChanger: false }"
        :scroll="{ x: 1100 }"
        row-key="ipAddress"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'cltt'">{{ fmtTime(record.cltt) }}</template>
          <template v-else-if="column.key === 'remaining'">
            <Tag :color="(record.cltt + (record.validLifetime ?? 0)) > now ? 'green' : 'default'" class="m-0">
              {{ remaining(record) }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'state'">
            <Tag :color="(record.state ?? 0) === 0 ? 'green' : 'red'" class="m-0">
              {{ (record.state ?? 0) === 0 ? '正常' : '冲突/declined' }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="flex flex-wrap items-center gap-1">
              <Button size="small" type="link" @click="openExtend(record)">延期</Button>
              <Button size="small" type="link" @click="openRebind(record)">改绑</Button>
              <Popconfirm
                title="释放该租约？"
                :description="`客户端 ${record.hwAddress ?? ''} 将重新申请地址。`"
                ok-text="释放"
                cancel-text="取消"
                @confirm="release4(record)"
              >
                <Button size="small" type="link" danger>释放</Button>
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
            <Tag :color="record.leaseType === 'IA_PD' ? 'orange' : 'blue'" class="m-0">{{ record.leaseType }}</Tag>
          </template>
          <template v-else-if="column.key === 'cltt'">{{ fmtTime(record.cltt) }}</template>
          <template v-else-if="column.key === 'remaining'">{{ remaining(record) }}</template>
          <template v-else-if="column.key === 'actions'">
            <Popconfirm
              title="释放该 IPv6 租约？"
              :description="`${record.ipAddress}（${record.leaseType}）`"
              ok-text="释放"
              cancel-text="取消"
              @confirm="release6(record)"
            >
              <Button size="small" type="link" danger>释放</Button>
            </Popconfirm>
          </template>
        </template>
      </Table>
    </Card>

    <Modal
      v-model:open="extendOpen"
      title="租约延期"
      ok-text="保存"
      cancel-text="取消"
      :confirm-loading="extendSaving"
      @ok="submitExtend"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">地址</span>
          <Input :value="extendTarget?.ipAddress" disabled />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">新租约时长（秒）</span>
          <InputNumber v-model:value="extendValue" :min="60" :max="31536000" :step="600" style="width: 100%" />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          仅修改 Kea 运行态租约；客户端下次续约时以服务端策略为准。
        </div>
      </div>
    </Modal>

    <Modal
      v-model:open="rebindOpen"
      title="租约改绑"
      ok-text="保存"
      cancel-text="取消"
      :confirm-loading="rebindSaving"
      @ok="submitRebind"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">地址</span>
          <Input :value="rebindTarget?.ipAddress" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">新 MAC</span>
          <Input v-model:value="rebindForm.mac" placeholder="aa:bb:cc:dd:ee:ff" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">主机名</span>
          <Input v-model:value="rebindForm.hostname" placeholder="可选" />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          改绑仅更新现有租约归属；如需长期固定，请使用「保留与绑定」创建 host reservation。
        </div>
      </div>
    </Modal>
  </div>
</template>
