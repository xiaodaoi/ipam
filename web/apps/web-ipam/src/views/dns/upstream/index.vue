<script setup lang="ts">
import type { Upstream } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Popconfirm, Select, Switch, Table, Tag } from 'ant-design-vue';

import { createUpstream, deleteUpstream, listUpstreams, updateUpstream } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const showTotal = (t: number) => `共 ${t} 条`;

type Proto = 'dot' | 'tcp' | 'udp';
type Health = 'down' | 'unknown' | 'up';

const rows = ref<Upstream[]>([]);
const editingId = ref<string>();
const loading = ref(false);
const form = ref<{ addr: string; enabled: boolean; name: string; protocol: Proto; weight: number }>({
  name: '', addr: '', protocol: 'udp', weight: 1, enabled: true,
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
    message.warning('名称与地址必填');
    return;
  }
  const addrs = form.value.addr.split(',').map((s) => s.trim()).filter(Boolean);
  if (addrs.length === 0) {
    message.warning('请填写至少一个上游地址');
    return;
  }
  try {
    if (editingId.value) {
      await updateUpstream(editingId.value, {
        name: form.value.name,
        addrs,
        protocol: form.value.protocol,
        weight: form.value.weight,
        enabled: form.value.enabled,
      });
      message.success('上游已更新并下发 resolver');
    } else {
      await createUpstream({
        name: form.value.name,
        addrs,
        protocol: form.value.protocol,
        weight: form.value.weight,
        enabled: form.value.enabled,
      });
      message.success('上游已添加');
    }
    editingId.value = undefined;
    form.value = { name: '', addr: '', protocol: 'udp', weight: 1, enabled: true };
    formModalApi.close();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
function edit(r: Upstream) {
  editingId.value = r.id;
  formModalApi.setState({ title: '编辑上游', confirmText: '保存修改' });
  formModalApi.open();
  form.value = {
    name: r.name,
    addr: (r.addrs ?? []).join(','),
    protocol: (r.protocol as Proto) ?? 'udp',
    weight: r.weight ?? 1,
    enabled: r.enabled ?? true,
  };
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '上游 DNS 服务器', confirmText: '添加', onConfirm: () => add() });
function openAdd() {
  // 不能先 close()（异步回写 isOpen 会把刚打开的弹窗关掉）
  editingId.value = undefined;
  form.value = { name: '', addr: '', protocol: 'udp', weight: 1, enabled: true };
  formModalApi.setState({ title: '添加上游 DNS', confirmText: '添加' });
  formModalApi.open();
}
async function toggleEnabled(r: Upstream, checked: boolean) {
  try {
    await updateUpstream(r.id, { enabled: checked });
    message.success(checked ? '已启用' : '已停用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
  await load();
}
async function remove(id?: string) {
  if (!id) return;
  try {
    await deleteUpstream(id);
    message.success('已删除');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败');
  }
  await load();
}
onMounted(() => {
  void load();
  timer = setInterval(load, 15_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));

// ── 展示映射 ──
const PROTO_TEXT: Record<string, string> = { dot: 'DoT', tcp: 'TCP', udp: 'UDP' };
const PROTO_COLOR: Record<string, string> = { dot: 'purple', tcp: 'cyan', udp: 'blue' };
const HEALTH_TEXT: Record<Health, string> = { up: '在线', down: '已摘除', unknown: '探测中' };
const HEALTH_COLOR: Record<Health, string> = { up: 'green', down: 'red', unknown: 'default' };

function healthOf(r: Upstream): Health {
  if (r.health?.up === true) return 'up';
  if (r.health?.up === false) return 'down';
  return 'unknown';
}

function fmtTime(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  if (Number.isNaN(d.getTime()) || d.getFullYear() <= 1970) return '—';
  return d.toLocaleString('zh-CN', { hour12: false });
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
  const avgRtt = rtts.length > 0 ? rtts.reduce((a, b) => a + b, 0) / rtts.length : 0;
  const bestRtt = rtts.length > 0 ? Math.min(...rtts) : 0;
  return { avgRtt, bestRtt, down, enabled, total, unknown, up };
});

// ── 筛选 ──
const keyword = ref('');
const protoFilter = ref<'all' | Proto>('all');
const healthFilter = ref<'all' | Health>('all');
const protoOptions = [
  { label: '全部协议', value: 'all' },
  { label: 'UDP', value: 'udp' },
  { label: 'TCP', value: 'tcp' },
  { label: 'DoT', value: 'dot' },
];
const healthOptions = [
  { label: '全部状态', value: 'all' },
  { label: '在线', value: 'up' },
  { label: '已摘除', value: 'down' },
  { label: '探测中', value: 'unknown' },
];

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    if (protoFilter.value !== 'all' && r.protocol !== protoFilter.value) return false;
    if (healthFilter.value !== 'all' && healthOf(r) !== healthFilter.value) return false;
    if (!kw) return true;
    return `${r.name} ${(r.addrs ?? []).join(' ')}`.toLowerCase().includes(kw);
  });
});
function resetFilters() {
  keyword.value = '';
  protoFilter.value = 'all';
  healthFilter.value = 'all';
}
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:globe"
      title="上游 DNS 服务器"
      description="维护递归查询使用的上游服务器与协议，按健康探测结果自动摘除不可用节点。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:activity" class="size-3.5" />
          {{ stats.up }}/{{ stats.total }} 在线 · {{ stats.enabled }} 已启用
        </span>
        <Button size="small" :loading="loading" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" @click="openAdd()">
          <IconifyIcon icon="lucide:plus" class="mr-1" />添加上游
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="上游总数"
        :value="stats.total"
        unit="个"
        icon="lucide:server"
        tone="blue"
        :hint="`${stats.enabled} 个已启用 · ${stats.total - stats.enabled} 个停用`"
      />
      <StatCard
        label="在线上游"
        :value="stats.up"
        unit="个"
        icon="lucide:circle-check"
        tone="green"
        :progress="stats.total > 0 ? (stats.up / stats.total) * 100 : 0"
        :hint="stats.total === 0 ? '尚未配置上游' : `在线率 ${Math.round((stats.up / stats.total) * 100)}%`"
      />
      <StatCard
        label="异常 / 未探测"
        :value="stats.down + stats.unknown"
        unit="个"
        icon="lucide:triangle-alert"
        :tone="stats.down > 0 ? 'red' : stats.unknown > 0 ? 'amber' : 'green'"
        :hint="`已摘除 ${stats.down} · 探测中 ${stats.unknown}`"
      />
      <StatCard
        label="平均 RTT"
        :value="stats.avgRtt > 0 ? stats.avgRtt.toFixed(1) : '—'"
        :unit="stats.avgRtt > 0 ? 'ms' : ''"
        icon="lucide:timer"
        tone="violet"
        :hint="stats.bestRtt > 0 ? `最快 ${stats.bestRtt} ms` : '等待首次探测结果'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-tree" />
          上游服务器列表
          <em class="ipam-card-sub">{{ filtered.length }} / {{ rows.length }} 个</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">每 15 秒自动刷新健康状态</span>
      </template>

      <div class="ipam-summary mb-3">
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">在线</span>
          <span class="ipam-summary__value">{{ stats.up }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">已摘除</span>
          <span class="ipam-summary__value">{{ stats.down }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">探测中</span>
          <span class="ipam-summary__value">{{ stats.unknown }}<small>个</small></span>
        </div>
        <div class="ipam-summary__item">
          <span class="ipam-summary__label">平均 RTT</span>
          <span class="ipam-summary__value">{{ stats.avgRtt > 0 ? stats.avgRtt.toFixed(1) : '—' }}<small v-if="stats.avgRtt > 0">ms</small></span>
        </div>
      </div>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">关键字</span>
          <Input v-model:value="keyword" allow-clear placeholder="名称 / 地址" style="width: 220px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">协议</span>
          <Select v-model:value="protoFilter" :options="protoOptions" style="width: 130px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">健康状态</span>
          <Select v-model:value="healthFilter" :options="healthOptions" style="width: 130px" />
        </div>
        <Button size="small" @click="resetFilters">重置</Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">摘除中的上游不参与递归选路</span>
      </div>

      <Table
        :data-source="filtered"
        :loading="loading"
        :columns="[
          { title: '上游（名称 / 地址）', dataIndex: 'name', minWidth: 260 },
          { title: '协议 / 权重', dataIndex: 'protocol', width: 140 },
          { title: '健康', dataIndex: 'health', minWidth: 180 },
          { title: '最近探测', dataIndex: 'lastCheck', width: 170 },
          { title: '启用', dataIndex: 'enabled', width: 80 },
          { title: '操作', key: 'op', width: 140 },
        ]"
        :locale="{ emptyText: keyword || protoFilter !== 'all' || healthFilter !== 'all' ? '没有匹配的上游服务器' : '暂无上游服务器，点击右上角「添加上游」创建' }"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'name'">
            <div class="ipam-cell-stack">
              <span class="ipam-cell-title">{{ record.name }}</span>
              <span class="ipam-cell-meta ipam-mono">{{ (record.addrs ?? []).join(' · ') || '—' }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'protocol'">
            <div class="flex flex-wrap items-center gap-1">
              <Tag class="m-0" :color="PROTO_COLOR[record.protocol] ?? 'default'">
                {{ PROTO_TEXT[record.protocol] ?? record.protocol }}
              </Tag>
              <span class="ipam-chip" data-tone="muted">权重 {{ record.weight ?? 1 }}</span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'health'">
            <div class="flex flex-wrap items-center gap-1">
              <Tag class="m-0" :color="HEALTH_COLOR[healthOf(record as Upstream)]">
                {{ HEALTH_TEXT[healthOf(record as Upstream)] }}
              </Tag>
              <span
                v-if="record.health?.rttMs !== undefined && record.health?.rttMs !== null"
                class="ipam-chip ipam-mono"
                :data-tone="healthOf(record as Upstream) === 'up' ? 'green' : 'muted'"
              >
                {{ record.health.rttMs }} ms
              </span>
              <span v-if="(record.health?.consecutiveFails ?? 0) > 0" class="ipam-chip" data-tone="red">
                连续失败 {{ record.health.consecutiveFails }}
              </span>
            </div>
          </template>
          <template v-else-if="column.dataIndex === 'lastCheck'">
            <span class="ipam-hint">{{ fmtTime(record.health?.lastCheck) }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'enabled'">
            <Switch :checked="record.enabled" size="small" @change="(v: any) => toggleEnabled(record as Upstream, !!v)" />
          </template>
          <template v-else-if="column.key === 'op'">
            <div class="flex items-center gap-1">
              <Button size="small" @click="edit(record as Upstream)">编辑</Button>
              <Popconfirm title="确认删除该上游？" description="删除后不可恢复。" ok-text="删除" cancel-text="取消" @confirm="remove(record.id)">
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            </div>
          </template>
        </template>
      </Table>
    </Card>

    <FormModal class="w-[720px]">
      <div class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">名称<em>*</em></span>
          <Input v-model:value="form.name" placeholder="如 阿里公共 DNS" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">协议</span>
          <Select
            v-model:value="form.protocol"
            :options="[
              { value: 'udp', label: 'UDP（默认 53）' },
              { value: 'tcp', label: 'TCP' },
              { value: 'dot', label: 'DoT（853）' },
            ]"
          />
        </label>
        <label class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">地址<em>*</em></span>
          <Input v-model:value="form.addr" placeholder="可填多个，逗号分隔，如 223.5.5.5:53,223.6.6.6:53" />
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">权重</span>
          <InputNumber v-model:value="form.weight" :min="1" :max="100" style="width: 100%" />
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">启用</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="form.enabled" size="small" />
            <span class="ipam-hint">{{ form.enabled ? '参与递归选路' : '配置保留但不参与选路' }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          权重越高被选中的概率越大；DoT 需上游支持 853 端口 TLS 握手。
        </div>
      </div>
    </FormModal>
  </div>
</template>
