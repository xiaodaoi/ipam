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
    message.error(error instanceof Error ? error.message : '发现列表加载失败');
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
  claimForm.note = row.hostname ? `自动发现：${row.hostname}` : '自动发现终端';
  claimForm.orgId = undefined;
  claimOpen.value = true;
}

async function submitClaim() {
  if (!claimTarget.value) return;
  if (!claimForm.owner.trim()) {
    message.warning('请填写使用人/责任人');
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
    message.success('已登记资产');
    claimOpen.value = false;
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '登记失败');
  } finally {
    claimSaving.value = false;
  }
}

const columns = [
  { title: 'MAC', dataIndex: 'mac', key: 'mac', width: 170 },
  { title: 'IPv4', dataIndex: 'ipv4', key: 'ipv4', width: 140 },
  { title: 'IPv6', dataIndex: 'ipv6', key: 'ipv6', width: 240 },
  { title: '主机名', dataIndex: 'hostname', key: 'hostname', ellipsis: true },
  { title: '状态', dataIndex: 'state', key: 'state', width: 100 },
  { title: '最近活动', dataIndex: 'lastSeen', key: 'lastSeen', width: 180 },
  { title: '操作', key: 'actions', width: 100 },
];

onMounted(async () => {
  await Promise.all([load(), loadOrgs()]);
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Discovery"
      icon="lucide:radar"
      title="未登记终端"
      description="有 DHCP 绑定/租约、但资产表尚未登记的终端。登记后即可关联使用人、部门与组织。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:clock" class="size-3.5" />
          最近活动 {{ latestSeen }}
        </span>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard label="未登记终端" :value="rows.length" unit="台" icon="lucide:radar" tone="amber" hint="MAC 未出现在资产表" />
      <StatCard label="有 IPv4" :value="v4Count" unit="台" icon="lucide:network" tone="green" hint="来自 coherence 绑定" />
      <StatCard label="有 IPv6" :value="v6Count" unit="台" icon="lucide:globe" tone="cyan" hint="含双栈终端" />
      <StatCard label="最近活动" :value="latestSeen" icon="lucide:activity" tone="violet" hint="按租约 cltt" />
    </div>

    <Card>
      <div class="ipam-toolbar ipam-toolbar--plain mb-3">
        <span class="ipam-hint">仅展示 active/grace/conflict 绑定；登记后该终端从列表移除。</span>
        <span class="ipam-toolbar__spacer"></span>
        <Input v-model:value="keyword" allow-clear placeholder="搜索 MAC / IP / 主机名" style="width: 260px">
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
            <Button size="small" type="link" @click="openClaim(record)">登记</Button>
          </template>
        </template>
      </Table>
    </Card>

    <Modal
      v-model:open="claimOpen"
      title="登记未登记终端"
      ok-text="登记"
      cancel-text="取消"
      :confirm-loading="claimSaving"
      @ok="submitClaim"
    >
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">MAC</span>
          <Input :value="claimTarget?.mac" disabled />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">使用人 / 责任人 *</span>
          <Input v-model:value="claimForm.owner" placeholder="使用人 / 责任人" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">部门</span>
          <Input v-model:value="claimForm.dept" placeholder="部门（可选）" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">所属组织</span>
          <Select
            v-model:value="claimForm.orgId"
            allow-clear
            :options="orgOptions"
            placeholder="可选"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">备注</span>
          <Input v-model:value="claimForm.note" placeholder="备注（可选）" />
        </div>
      </div>
    </Modal>
  </div>
</template>
