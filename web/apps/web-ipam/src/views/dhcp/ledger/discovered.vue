<script setup lang="ts">
import type {
  AssetUpsert,
  DiscoveredTerminal,
  IgnoredTerminal,
  OrgTreeNode,
} from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  message,
  Modal,
  Popconfirm,
  Select,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  bulkUpsertAssets,
  deleteIgnoredTerminal,
  ignoreTerminal,
  listDiscoveredTerminals,
  listIgnoredTerminals,
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
const claimForm = reactive({
  dept: '',
  note: '',
  orgId: undefined as string | undefined,
  owner: '',
});

// ── 多选批量登记（M6-047）──
const selectedKeys = ref<string[]>([]);
const bulkOpen = ref(false);
const bulkSaving = ref(false);
const bulkForm = reactive({
  dept: '',
  orgId: undefined as string | undefined,
  owner: '',
});

// ── 忽略名单（M6-047）──
const ignoredOpen = ref(false);
const ignoredLoading = ref(false);
const ignoredRows = ref<IgnoredTerminal[]>([]);

const selectedRows = computed(() =>
  rows.value.filter((r) => selectedKeys.value.includes(r.identity)),
);
const rowSelection = computed(() => ({
  selectedRowKeys: selectedKeys.value,
  onChange: (keys: (number | string)[]) => {
    selectedKeys.value = keys.map(String);
  },
}));

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

/** 组织 id → 完整路径（发现列表里展示终端归属，登记后可核对） */
const orgPathMap = computed(() => {
  const m = new Map<string, string[]>();
  const walk = (nodes: OrgTreeNode[], trail: string[]) => {
    for (const n of nodes) {
      const next = [...trail, n.name];
      m.set(n.id, next);
      if (n.children?.length) walk(n.children, next);
    }
  };
  walk(orgTree.value, []);
  return m;
});

function orgPathOf(orgId?: string): string {
  return orgId ? (orgPathMap.value.get(orgId)?.join(' / ') ?? '') : '';
}

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return rows.value;
  // 支持省略分隔符的 MAC/DUID 输入（库里是带冒号格式）。
  const compact = kw.replaceAll(/[:\-.\s]/g, '');
  return rows.value.filter((r) =>
    [r.identity, r.mac, r.duid, r.ipv4, r.ipv6, r.hostname, r.subnetCidr]
      .filter(Boolean)
      .some((v) => {
        const s = String(v).toLowerCase();
        return (
          s.includes(kw) ||
          (compact.length >= 4 && s.replaceAll(':', '').includes(compact))
        );
      }),
  );
});

const v4Count = computed(() => rows.value.filter((r) => r.ipv4).length);
const v6Count = computed(() => rows.value.filter((r) => r.ipv6).length);
const duidCount = computed(
  () => rows.value.filter((r) => !r.mac && r.duid).length,
);
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
    // 已登记/已忽略的终端会从列表消失，同步剔除失效的选中项。
    const alive = new Set(rows.value.map((r) => r.identity));
    selectedKeys.value = selectedKeys.value.filter((k) => alive.has(k));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.discovered.loadFailed'),
    );
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

async function loadIgnored() {
  ignoredLoading.value = true;
  try {
    const res = await listIgnoredTerminals();
    ignoredRows.value = res.items ?? [];
  } catch {
    ignoredRows.value = [];
  } finally {
    ignoredLoading.value = false;
  }
}

function openIgnored() {
  ignoredOpen.value = true;
  void loadIgnored();
}

function openBulk() {
  if (selectedRows.value.length === 0) return;
  bulkForm.owner = '';
  bulkForm.dept = '';
  // 选中终端同属一个子网组织时默认带入，否则留空由用户选择。
  const orgIds = new Set(selectedRows.value.map((r) => r.orgId ?? ''));
  bulkForm.orgId =
    orgIds.size === 1 ? (selectedRows.value[0]?.orgId ?? undefined) : undefined;
  bulkOpen.value = true;
}

async function submitBulk() {
  if (!bulkForm.owner.trim()) {
    message.warning($t('dhcpLedger.discovered.ownerRequired'));
    return;
  }
  const owner = bulkForm.owner.trim();
  const dept = bulkForm.dept.trim() || undefined;
  const items: AssetUpsert[] = selectedRows.value.map((r) => ({
    mac: r.mac || undefined,
    duid: r.duid || undefined,
    owner,
    dept,
    orgId: bulkForm.orgId,
    note: r.hostname
      ? $t('dhcpLedger.discovered.autoNote', { hostname: r.hostname })
      : undefined,
  }));
  bulkSaving.value = true;
  try {
    const res = await bulkUpsertAssets(items);
    if (res.failures?.length) {
      message.warning(
        $t('dhcpLedger.discovered.bulkPartial', {
          failed: res.failures.length,
          saved: res.saved,
        }),
      );
    } else {
      message.success(
        $t('dhcpLedger.discovered.bulkDone', { count: res.saved }),
      );
    }
    bulkOpen.value = false;
    selectedKeys.value = [];
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.discovered.registerFailed'),
    );
  } finally {
    bulkSaving.value = false;
  }
}

/** 忽略：无需登记资产的打印机/AP 等，抑制后发现列表不再出现。 */
async function ignoreRow(row: DiscoveredTerminal) {
  try {
    await ignoreTerminal({
      mac: row.mac || undefined,
      duid: row.duid || undefined,
    });
    message.success($t('dhcpLedger.discovered.ignoredDone'));
    selectedKeys.value = selectedKeys.value.filter((k) => k !== row.identity);
    await Promise.all([load(), loadIgnored()]);
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.discovered.ignoreFailed'),
    );
  }
}

async function ignoreSelected() {
  const targets = [...selectedRows.value];
  let failed = 0;
  for (const row of targets) {
    try {
      await ignoreTerminal({
        mac: row.mac || undefined,
        duid: row.duid || undefined,
      });
    } catch {
      failed += 1;
    }
  }
  if (failed > 0) {
    message.warning(
      $t('dhcpLedger.discovered.ignorePartial', {
        failed,
        saved: targets.length - failed,
      }),
    );
  } else {
    message.success(
      $t('dhcpLedger.discovered.ignoreDone', { count: targets.length }),
    );
  }
  selectedKeys.value = [];
  await Promise.all([load(), loadIgnored()]);
}

async function restoreIgnored(it: IgnoredTerminal) {
  try {
    await deleteIgnoredTerminal(it.identity);
    message.success($t('dhcpLedger.discovered.restored'));
    await Promise.all([load(), loadIgnored()]);
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.discovered.restoreFailed'),
    );
  }
}

function openClaim(row: Partial<DiscoveredTerminal>) {
  if (!row.identity) return;
  claimTarget.value = row as DiscoveredTerminal;
  claimForm.owner = '';
  claimForm.dept = '';
  claimForm.note = row.hostname
    ? $t('dhcpLedger.discovered.autoNote', { hostname: row.hostname })
    : $t('dhcpLedger.discovered.autoNotePlain');
  // 组织默认取子网归属，减少手工选择（M6-046）。
  claimForm.orgId = row.orgId || undefined;
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
      mac: claimTarget.value.mac || undefined,
      duid: claimTarget.value.duid || undefined,
      owner: claimForm.owner.trim(),
      dept: claimForm.dept.trim() || undefined,
      note: claimForm.note.trim() || undefined,
      orgId: claimForm.orgId,
    });
    message.success($t('dhcpLedger.discovered.registered'));
    claimOpen.value = false;
    await load();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('dhcpLedger.discovered.registerFailed'),
    );
  } finally {
    claimSaving.value = false;
  }
}

const columns = computed(() => [
  {
    title: $t('dhcpLedger.discovered.colIdentity'),
    dataIndex: 'identity',
    key: 'identity',
    minWidth: 210,
  },
  {
    title: $t('dhcpLedger.discovered.colIpv4'),
    dataIndex: 'ipv4',
    key: 'ipv4',
    width: 130,
  },
  {
    title: $t('dhcpLedger.discovered.colIpv6'),
    dataIndex: 'ipv6',
    key: 'ipv6',
    minWidth: 220,
  },
  {
    title: $t('dhcpLedger.discovered.colHostname'),
    dataIndex: 'hostname',
    key: 'hostname',
    ellipsis: true,
  },
  {
    title: $t('dhcpLedger.discovered.colSubnet'),
    dataIndex: 'subnetCidr',
    key: 'subnetCidr',
    minWidth: 140,
  },
  {
    title: $t('dhcpLedger.discovered.colOrg'),
    dataIndex: 'orgId',
    key: 'orgId',
    minWidth: 150,
  },
  {
    title: $t('dhcpLedger.discovered.colState'),
    dataIndex: 'state',
    key: 'state',
    width: 90,
  },
  {
    title: $t('dhcpLedger.discovered.colLastSeen'),
    dataIndex: 'lastSeen',
    key: 'lastSeen',
    width: 170,
  },
  {
    title: $t('dhcpLedger.discovered.colActions'),
    key: 'actions',
    width: 150,
  },
]);

const ignoredColumns = computed(() => [
  {
    title: $t('dhcpLedger.discovered.colIdentity'),
    dataIndex: 'identity',
    key: 'identity',
    minWidth: 220,
  },
  {
    title: $t('dhcpLedger.discovered.colReason'),
    dataIndex: 'reason',
    key: 'reason',
  },
  {
    title: $t('dhcpLedger.discovered.colIgnoredAt'),
    dataIndex: 'createdAt',
    key: 'createdAt',
    width: 170,
  },
  {
    title: $t('dhcpLedger.discovered.colActions'),
    key: 'actions',
    width: 80,
  },
]);

onMounted(async () => {
  await Promise.all([load(), loadOrgs(), loadIgnored()]);
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
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('dhcpLedger.common.refresh')
          }}
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
        :hint="$t('dhcpLedger.discovered.statV6Hint', { count: duidCount })"
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
        <span class="ipam-hint">{{
          $t('dhcpLedger.discovered.listHint')
        }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <template v-if="selectedKeys.length > 0">
          <Button type="primary" size="small" @click="openBulk">
            {{
              $t('dhcpLedger.discovered.bulkRegister', {
                count: selectedKeys.length,
              })
            }}
          </Button>
          <Popconfirm
            :title="$t('dhcpLedger.discovered.ignoreSelectedTitle')"
            :ok-text="$t('dhcpLedger.discovered.ignore')"
            :cancel-text="$t('dhcpLedger.common.cancel')"
            @confirm="ignoreSelected"
          >
            <Button size="small">
              {{
                $t('dhcpLedger.discovered.ignoreSelected', {
                  count: selectedKeys.length,
                })
              }}
            </Button>
          </Popconfirm>
        </template>
        <Button size="small" @click="openIgnored">
          {{ $t('dhcpLedger.discovered.ignoredList') }}
          <span v-if="ignoredRows.length > 0">({{ ignoredRows.length }})</span>
        </Button>
        <Input
          v-model:value="keyword"
          allow-clear
          :placeholder="$t('dhcpLedger.discovered.keywordPlaceholder')"
          style="width: 260px"
        >
          <template #prefix>
            <IconifyIcon icon="lucide:search" class="size-3.5" />
          </template>
        </Input>
      </div>

      <Table
        :columns="columns"
        :data-source="filtered"
        :loading="loading"
        :pagination="{ pageSize: 20, showSizeChanger: true }"
        :row-key="(r: DiscoveredTerminal) => r.identity"
        :row-selection="rowSelection"
        size="small"
        :scroll="{ x: 1260 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'identity'">
            <div class="ipam-cell-stack">
              <span v-if="record.mac" class="ipam-mono">{{ record.mac }}</span>
              <span v-else class="ipam-chip" data-tone="violet">
                {{ $t('dhcpLedger.discovered.duidOnly') }}
              </span>
              <span
                v-if="record.duid"
                class="ipam-cell-meta ipam-mono"
                :title="record.duid"
              >
                DUID {{ record.duid }}
              </span>
            </div>
          </template>
          <template v-else-if="column.key === 'ipv4'">
            <span class="ipam-mono">{{ record.ipv4 || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'ipv6'">
            <span class="ipam-mono">{{ record.ipv6 || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'subnetCidr'">
            <span class="ipam-mono">{{ record.subnetCidr || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'orgId'">
            <span
              v-if="record.orgId"
              class="ipam-org-path ipam-org-path--start"
              :title="orgPathOf(record.orgId)"
            >
              {{ orgPathOf(record.orgId) }}
            </span>
            <span v-else class="text-gray-400">—</span>
          </template>
          <template v-else-if="column.key === 'state'">
            <Tag :color="stateColor(record.state)" class="m-0">
              {{ record.state || 'active' }}
            </Tag>
          </template>
          <template v-else-if="column.key === 'lastSeen'">
            {{ fmtTime(record.lastSeen) }}
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="flex items-center gap-1">
              <Button size="small" type="link" @click="openClaim(record)">
                {{ $t('dhcpLedger.discovered.register') }}
              </Button>
              <Popconfirm
                :title="$t('dhcpLedger.discovered.ignoreConfirmTitle')"
                :description="$t('dhcpLedger.discovered.ignoreConfirmDesc')"
                :ok-text="$t('dhcpLedger.discovered.ignore')"
                :cancel-text="$t('dhcpLedger.common.cancel')"
                @confirm="ignoreRow(record as DiscoveredTerminal)"
              >
                <Button size="small" type="link" danger>
                  {{ $t('dhcpLedger.discovered.ignore') }}
                </Button>
              </Popconfirm>
            </div>
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
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.identity')
          }}</span>
          <Input :value="claimTarget?.identity" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.ownerLabel')
          }}</span>
          <Input
            v-model:value="claimForm.owner"
            :placeholder="$t('dhcpLedger.discovered.form.ownerPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.dept')
          }}</span>
          <Input
            v-model:value="claimForm.dept"
            :placeholder="$t('dhcpLedger.discovered.form.deptPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.org')
          }}</span>
          <Select
            v-model:value="claimForm.orgId"
            allow-clear
            :options="orgOptions"
            :placeholder="$t('dhcpLedger.discovered.form.orgPlaceholder')"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.note')
          }}</span>
          <Input
            v-model:value="claimForm.note"
            :placeholder="$t('dhcpLedger.discovered.form.notePlaceholder')"
          />
        </div>
      </div>
    </Modal>

    <Modal
      v-model:open="bulkOpen"
      :title="
        $t('dhcpLedger.discovered.bulkTitle', { count: selectedKeys.length })
      "
      :ok-text="$t('dhcpLedger.discovered.bulkSubmit')"
      :cancel-text="$t('dhcpLedger.common.cancel')"
      :confirm-loading="bulkSaving"
      @ok="submitBulk"
    >
      <div class="ipam-hint mb-2">
        {{ $t('dhcpLedger.discovered.bulkHint') }}
      </div>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.ownerLabel')
          }}</span>
          <Input
            v-model:value="bulkForm.owner"
            :placeholder="$t('dhcpLedger.discovered.form.ownerPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.dept')
          }}</span>
          <Input
            v-model:value="bulkForm.dept"
            :placeholder="$t('dhcpLedger.discovered.form.deptPlaceholder')"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">{{
            $t('dhcpLedger.discovered.form.org')
          }}</span>
          <Select
            v-model:value="bulkForm.orgId"
            allow-clear
            :options="orgOptions"
            :placeholder="$t('dhcpLedger.discovered.form.orgPlaceholder')"
          />
        </div>
      </div>
    </Modal>

    <Modal
      v-model:open="ignoredOpen"
      :footer="null"
      :title="$t('dhcpLedger.discovered.ignoredTitle')"
      :width="760"
    >
      <div class="ipam-hint mb-2">
        {{ $t('dhcpLedger.discovered.ignoredHint') }}
      </div>
      <Table
        :columns="ignoredColumns"
        :data-source="ignoredRows"
        :loading="ignoredLoading"
        :pagination="{ pageSize: 10 }"
        :row-key="(r: IgnoredTerminal) => r.identity"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'identity'">
            <span class="ipam-mono">{{ record.mac || record.duid }}</span>
          </template>
          <template v-else-if="column.key === 'reason'">
            {{ record.reason || '—' }}
          </template>
          <template v-else-if="column.key === 'createdAt'">
            {{ fmtTime(record.createdAt) }}
          </template>
          <template v-else-if="column.key === 'actions'">
            <Button
              size="small"
              type="link"
              @click="restoreIgnored(record as IgnoredTerminal)"
            >
              {{ $t('dhcpLedger.discovered.restore') }}
            </Button>
          </template>
        </template>
      </Table>
    </Modal>
  </div>
</template>
