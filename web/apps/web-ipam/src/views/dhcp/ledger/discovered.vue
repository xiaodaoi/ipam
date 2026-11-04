<script setup lang="ts">
import type { DiscoveredTerminal, OrgTreeNode } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  message,
  Modal,
  Select,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  listDiscoveredTerminals,
  listOrgTree,
  upsertAsset,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';

defineOptions({ name: 'DhcpDiscovered' });

const loading = ref(false);
const rows = ref<DiscoveredTerminal[]>([]);
const keyword = ref('');
const orgTree = ref<OrgTreeNode[]>([]);

const claimOpen = ref(false);
const claimTarget = ref<DiscoveredTerminal>();
const claimSaving = ref(false);
const claimForm = reactive({ dept: '', note: '', orgId: undefined as string | undefined, owner: '' });

const orgOptions = computed(() => {
  const out: { label: string; value: string }[] = [];
  const walk = (nodes: OrgTreeNode[], depth: number) => {
    for (const n of nodes) {
      out.push({ label: `${'　'.repeat(depth)}${n.name}`, value: n.id });
      if (n.children?.length) walk(n.children, depth + 1);
    }
  };
  walk(orgTree.value, 0);
  return out;
});

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return rows.value;
  return rows.value.filter((r) =>
    [r.mac, r.ipv4, r.ipv6, r.hostname]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(kw)),
  );
});

const v4Count = computed(() => rows.value.filter((r) => r.ipv4).length);
const v6Count = computed(() => rows.value.filter((r) => r.ipv6).length);
const latestSeen = computed(() => {
  let latest = 0;
  for (const r of rows.value) {
    if (!r.lastSeen) continue;
    const t = new Date(r.lastSeen).getTime();
    if (t > latest) latest = t;
  }
  return latest ? new Date(latest).toLocaleString() : '—';
});

function fmtTime(ts?: string) {
  return ts ? new Date(ts).toLocaleString() : '—';
}

function stateColor(state?: string) {
  if (state === 'conflict') return 'red';
  if (state === 'grace') return 'orange';
  return 'green';
}

async function load() {
  loading.value = true;
  try {
    const res = await listDiscoveredTerminals();
    rows.value = res.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('dhcpLedger.discovered.loadFailed'));
  } finally {
    loading.value = false;
  }
}

async function loadOrgs() {
  try {
    orgTree.value = await listOrgTree();
  } catch {
    orgTree.value = [];
  }
}

function openClaim(row: Partial<DiscoveredTerminal>) {
  if (!row.mac) return;
  claimTarget.value = row as DiscoveredTerminal;
  claimForm.owner = '';
  claimForm.dept = '';
  claimForm.note = row.hostname
    ? $t('dhcpLedger.discovered.autoNote', { hostname: row.hostname })
    : $t('dhcpLedger.discovered.autoNotePlain');
  claimForm.orgId = undefined;
  claimOpen.value = true;
}

async function submitClaim() {
  if (!claimTarget.value) return;
  if (!claimForm.owner.trim()) {
    message.warning($t('dhcpLedger.discovered.ownerRequired'));
    return;
  }
  claimSaving.value = true;
  try {
    await upsertAsset({
      mac: claimTarget.value.mac,
      owner: claimForm.owner.trim(),
      dept: claimForm.dept.trim() || undefined,
      note: claimForm.note.trim() || undefined,
      orgId: claimForm.orgId,
    });
    message.success($t('dhcpLedger.discovered.registered'));
    claimOpen.value = false;
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('dhcpLedger.discovered.registerFailed'));
  } finally {
    claimSaving.value = false;
  }
}

const columns = computed(() => [
  { title: $t('dhcpLedger.discovered.colMac'), dataIndex: 'mac', key: 'mac', width: 170 },
  { title: $t('dhcpLedger.discovered.colIpv4'), dataIndex: 'ipv4', key: 'ipv4', width: 140 },
  { title: $t('dhcpLedger.discovered.colIpv6'), dataIndex: 'ipv6', key: 'ipv6', width: 240 },
  { title: $t('dhcpLedger.discovered.colHostname'), dataIndex: 'hostname', key: 'hostname', ellipsis: true },
  { title: $t('dhcpLedger.discovered.colState'), dataIndex: 'state', key: 'state', width: 100 },
  { title: $t('dhcpLedger.discovered.colLastSeen'), dataIndex: 'lastSeen', key: 'lastSeen', width: 180 },
  { title: $t('dhcpLedger.discovered.colActions'), key: 'actions', width: 100 },
]);

onMounted(async () => {
  await Promise.all([load(), loadOrgs()]);
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Discovery"
      icon="lucide:radar"
      :title="$t('dhcpLedger.discovered.title')"
      :description="$t('dhcpLedger.discovered.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock" class="size-3.5" />
          {{ $t('dhcpLedger.discovered.meta', { time: latestSeen }) }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{ $t('dhcpLedger.common.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('dhcpLedger.discovered.statUnregistered')"
        :value="rows.length"
        :unit="$t('dhcpLedger.common.units.device')"
        icon="lucide:radar"
        tone="amber"
        :hint="$t('dhcpLedger.discovered.statUnregisteredHint')"
      />
      <StatCard
        :label="$t('dhcpLedger.discovered.statV4')"
        :value="v4Count"
        :unit="$t('dhcpLedger.common.units.device')"
        icon="lucide:network"
        tone="green"
        :hint="$t('dhcpLedger.discovered.statV4Hint')"
      />
      <StatCard
        :label="$t('dhcpLedger.discovered.statV6')"
        :value="v6Count"
        :unit="$t('dhcpLedger.common.units.device')"
        icon="lucide:globe"
        tone="cyan"
        :hint="$t('dhcpLedger.discovered.statV6Hint')"
      />
      <StatCard
        :label="$t('dhcpLedger.discovered.statLatest')"
        :value="latestSeen"
        icon="lucide:activity"
        tone="violet"
        :hint="$t('dhcpLedger.discovered.statLatestHint')"
      />
    </div>

    <Card>
      <div class="ipam-toolbar ipam-toolbar--plain mb-3">
        <span class="ipam-hint">{{ $t('dhcpLedger.discovered.listHint') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Input
          v-model:value="keyword"
          allow-clear
          :placeholder="$t('dhcpLedger.discovered.keywordPlaceholder')"
          style="width: 260px"
        >
          <template #prefix><IconifyIcon icon="lucide:search" class="size-3.5" /></template>
        </Input>
      </div>

      <Table
        :columns="columns"
        :data-source="filtered"
        :loading="loading"
        :pagination="{ pageSize: 20, showSizeChanger: false }"
        row-key="mac"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'mac'"><span class="ipam-mono">{{ record.mac }}</span></template>
          <template v-else-if="column.key === 'ipv4'">
            <span class="ipam-mono">{{ record.ipv4 || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'ipv6'">
            <span class="ipam-mono">{{ record.ipv6 || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'state'">
            <Tag :color="stateColor(record.state)" class="m-0">{{ record.state || 'active' }}</Tag>
          </template>
          <template v-else-if="column.key === 'lastSeen'">{{ fmtTime(record.lastSeen) }}</template>
          <template v-else-if="column.key === 'actions'">
            <Button size="small" type="link" @click="openClaim(record)">{{ $t('dhcpLedger.discovered.register') }}</Button>
          </template>
        </template>
      </Table>
    </Card>

    <Modal
      v-model:open="claimOpen"
      :title="$t('dhcpLedger.discovered.modalTitle')"
      :ok-text="$t('dhcpLedger.discovered.register')"
      :cancel-text="$t('dhcpLedger.common.cancel')"
      :confirm-loading="claimSaving"
      @ok="submitClaim"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcpLedger.discovered.form.mac') }}</span>
          <Input :value="claimTarget?.mac" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcpLedger.discovered.form.ownerLabel') }}</span>
          <Input
            v-model:value="claimForm.owner"
            :placeholder="$t('dhcpLedger.discovered.form.ownerPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcpLedger.discovered.form.dept') }}</span>
          <Input
            v-model:value="claimForm.dept"
            :placeholder="$t('dhcpLedger.discovered.form.deptPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{ $t('dhcpLedger.discovered.form.org') }}</span>
          <Select
            v-model:value="claimForm.orgId"
            allow-clear
            :options="orgOptions"
            :placeholder="$t('dhcpLedger.discovered.form.orgPlaceholder')"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{ $t('dhcpLedger.discovered.form.note') }}</span>
          <Input
            v-model:value="claimForm.note"
            :placeholder="$t('dhcpLedger.discovered.form.notePlaceholder')"
          />
        </div>
      </div>
    </Modal>
  </div>
</template>
