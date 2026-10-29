<script setup lang="ts">
import type { DnsSettings } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Popconfirm, Switch, Table } from 'ant-design-vue';

import { flushCache, getDnsSettings, getLogQps, listTtlOverrides, updateDnsSettings, upsertTtlOverride } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const showTotal = (t: number) => `共 ${t} 条`;

const settings = ref<DnsSettings>();
const ttlRows = ref<{ domain: string; ttl: number }[]>([]);
const newTtl = ref({ domain: '', ttl: 300 });
const flushZone = ref('');
const saving = ref(false);
const flushing = ref(false);
const loading = ref(false);

const qps1h = ref(0);
const qpsTotal = ref(0);
const qpsPeak = ref(0);
const qpsSpark = ref<number[]>([]);

async function load() {
  loading.value = true;
  try {
    settings.value = await getDnsSettings();
    const d = await listTtlOverrides();
    ttlRows.value = (d.items ?? []) as { domain: string; ttl: number }[];
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (!settings.value) return;
  saving.value = true;
  try {
    settings.value = await updateDnsSettings(settings.value);
    message.success('参数已保存并经 checkconf-reload 生效');
  } finally {
    saving.value = false;
  }
}
async function doFlush() {
  flushing.value = true;
  try {
    const r = await flushCache(flushZone.value || undefined);
    message.success(`已清空：${r.flushed}（${r.cmd}）`);
    flushZone.value = '';
  } catch (error) {
    message.error(error instanceof Error ? error.message : '清空失败');
  } finally {
    flushing.value = false;
  }
}
async function addTtl() {
  if (!newTtl.value.domain || !newTtl.value.ttl) {
    message.warning('域名与 TTL 必填');
    return;
  }
  try {
    await upsertTtlOverride(newTtl.value);
    message.success('TTL 覆盖已保存');
    newTtl.value = { domain: '', ttl: 300 };
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  }
}

async function loadQps() {
  try {
    const from = new Date(Date.now() - 24 * 3_600_000).toISOString();
    const d = await getLogQps({ from, intervalSec: 3600 });
    const pts = d.points ?? [];
    const counts = pts.map((p) => p.count);
    qpsTotal.value = counts.reduce((a, b) => a + b, 0);
    qps1h.value = counts.slice(-1)[0] ?? 0;
    qpsPeak.value = counts.length > 0 ? Math.max(...counts) : 0;
    qpsSpark.value = counts.slice(-12);
  } catch {
    /* QPS 数据不可用时保持 0 */
  }
}
onMounted(() => {
  void load();
  void loadQps();
});

const stats = computed(() => ({
  avgPerHour: qpsTotal.value > 0 ? Math.round(qpsTotal.value / 24) : 0,
  covers: ttlRows.value.length,
  maxTtl: settings.value?.cacheMaxTtl ?? 0,
  minTtl: settings.value?.cacheMinTtl ?? 0,
  peak: qpsPeak.value,
  qps1h: qps1h.value,
  qpsTotal: qpsTotal.value,
  serveExpired: settings.value?.serveExpired ?? false,
}));

const nf = (n: number) => n.toLocaleString('zh-CN');

const ttlCols = [
  { title: '域名', dataIndex: 'domain', minWidth: 260 },
  { title: 'TTL（秒）', dataIndex: 'ttl', width: 120 },
  { title: '折合', dataIndex: 'human', width: 140 },
];

function humanTtl(ttl: number): string {
  if (ttl >= 86_400) return `${(ttl / 86_400).toFixed(ttl % 86_400 === 0 ? 0 : 1)} 天`;
  if (ttl >= 3600) return `${(ttl / 3600).toFixed(ttl % 3600 === 0 ? 0 : 1)} 小时`;
  if (ttl >= 60) return `${(ttl / 60).toFixed(ttl % 60 === 0 ? 0 : 1)} 分钟`;
  return `${ttl} 秒`;
}
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:gauge"
      title="缓存与性能"
      description="调整 resolver 缓存 TTL、过期兜底与单域名覆盖策略，并查看查询量概览。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:database" class="size-3.5" />
          TTL {{ stats.minTtl }}–{{ stats.maxTtl }}s · {{ stats.covers }} 条覆盖
        </span>
        <Button size="small" :loading="loading" @click="load(); loadQps()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">保存并生效</Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="最近 1 小时查询"
        :value="nf(stats.qps1h)"
        unit="次"
        icon="lucide:activity"
        tone="blue"
        :spark="qpsSpark.length > 1 ? qpsSpark : undefined"
        :hint="stats.qps1h > 0 ? '按小时聚合的最近一个窗口' : '暂无查询数据'"
      />
      <StatCard
        label="近 24 小时总查询"
        :value="nf(stats.qpsTotal)"
        unit="次"
        icon="lucide:bar-chart-3"
        tone="cyan"
        :hint="`每小时均值 ${nf(stats.avgPerHour)} 次`"
      />
      <StatCard
        label="小时峰值"
        :value="nf(stats.peak)"
        unit="次"
        icon="lucide:trending-up"
        tone="violet"
        :hint="stats.peak > 0 ? '窗口内单小时最高查询量' : '等待数据采集'"
      />
      <StatCard
        label="TTL 覆盖"
        :value="stats.covers"
        unit="条"
        icon="lucide:timer-reset"
        tone="green"
        :hint="stats.covers > 0 ? '单域名覆盖优先于全局 TTL' : '尚未设置单域名覆盖'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:settings-2" />
          缓存参数
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">保存后经 resolver 配置重载生效</span>
      </template>

      <div v-if="settings" class="ipam-form-grid">
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">最小 TTL（秒）</span>
          <InputNumber v-model:value="settings.cacheMinTtl" :min="0" style="width: 100%" />
          <span class="ipam-hint">0 表示不限制，按应答原始 TTL 缓存</span>
        </label>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">最大 TTL（秒）</span>
          <InputNumber v-model:value="settings.cacheMaxTtl" :min="1" style="width: 100%" />
          <span class="ipam-hint">应答 TTL 超过该值时按此值截断</span>
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">serve-expired（过期旧值兜底）</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.serveExpired" size="small" />
            <span class="ipam-hint">{{ settings.serveExpired ? '上游超时先应答过期缓存' : '过期即重新递归' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">当前策略</span>
          <div class="flex h-8 flex-wrap items-center gap-1">
            <span class="ipam-chip" data-tone="muted">min {{ settings.cacheMinTtl }}s</span>
            <span class="ipam-chip" data-tone="muted">max {{ settings.cacheMaxTtl }}s</span>
            <span class="ipam-chip" :data-tone="settings.serveExpired ? 'green' : 'muted'">
              serve-expired {{ settings.serveExpired ? 'ON' : 'OFF' }}
            </span>
          </div>
        </div>
        <div class="ipam-form-field--wide flex justify-end">
          <Button type="primary" :loading="saving" @click="save">
            <IconifyIcon icon="lucide:save" class="mr-1" />保存并生效
          </Button>
        </div>
      </div>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">参数加载中</div>
        <div class="ipam-empty__desc">若长时间无响应，请检查控制面连通性后重试</div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:eraser" />
          手动清空缓存
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">unbound-control flush / flush_zone</span>
      </template>

      <div class="ipam-toolbar">
        <div class="ipam-field">
          <span class="ipam-field__label">目标 zone</span>
          <Input v-model:value="flushZone" allow-clear placeholder="留空表示清空全部缓存" style="width: 280px" />
        </div>
        <Popconfirm
          title="确认清空缓存？"
          :description="flushZone ? `将清空 ${flushZone} 的缓存条目。` : '将清空全部缓存，可能造成瞬时回源压力。'"
          ok-text="清空"
          cancel-text="取消"
          @confirm="doFlush"
        >
          <Button danger :loading="flushing">
            <IconifyIcon icon="lucide:trash-2" class="mr-1" />清空缓存
          </Button>
        </Popconfirm>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">清空后首个查询会重新回源上游</span>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:timer-reset" />
          每域名 TTL 覆盖
          <em class="ipam-card-sub">{{ ttlRows.length }} 条</em>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">优先级高于全局缓存 TTL</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">域名</span>
          <Input v-model:value="newTtl.domain" placeholder="如 update.corp.local." style="width: 280px" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">TTL（秒）</span>
          <InputNumber v-model:value="newTtl.ttl" :min="1" style="width: 140px" />
        </div>
        <Button type="primary" size="small" @click="addTtl">
          <IconifyIcon icon="lucide:plus" class="mr-1" />设置覆盖
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span class="ipam-hint">相同域名重复提交将覆盖原值</span>
      </div>

      <Table
        v-if="ttlRows.length > 0"
        :data-source="ttlRows"
        :columns="ttlCols"
        :pagination="{ pageSize: 10, showTotal }"
        row-key="domain"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'domain'">
            <span class="ipam-mono text-xs">{{ record.domain }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'ttl'">
            <span class="ipam-chip ipam-mono" data-tone="blue">{{ record.ttl }}s</span>
          </template>
          <template v-else-if="column.dataIndex === 'human'">
            <span class="ipam-hint">{{ humanTtl(record.ttl) }}</span>
          </template>
        </template>
      </Table>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">暂无 TTL 覆盖</div>
        <div class="ipam-empty__desc">为更新频繁的域名设置较短 TTL，可在上方表单添加</div>
      </div>
    </Card>
  </div>
</template>
