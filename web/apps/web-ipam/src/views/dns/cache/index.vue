<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { Button, Card, Input, InputNumber, message, Switch, Table } from 'ant-design-vue';

import {
  flushCache,
  getDnsSettings,
  getLogQps,
  listTtlOverrides,
  updateDnsSettings,
  upsertTtlOverride,
  type DnsSettings,
} from '#/api/ipam';

const settings = ref<DnsSettings>();
const ttlRows = ref<{ domain: string; ttl: number }[]>([]);
const newTtl = ref({ domain: '', ttl: 300 });
const flushZone = ref('');
const saving = ref(false);

async function load() {
  settings.value = await getDnsSettings();
  const d = await listTtlOverrides();
  ttlRows.value = (d.items ?? []) as { domain: string; ttl: number }[];
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
  const r = await flushCache(flushZone.value || undefined);
  message.success(`已清空：${r.flushed}（${r.cmd}）`);
}
async function addTtl() {
  if (!newTtl.value.domain || !newTtl.value.ttl) return;
  await upsertTtlOverride(newTtl.value);
  newTtl.value = { domain: '', ttl: 300 };
  await load();
}
const qps1h = ref(0);
const qpsTotal = ref(0);
const qpsPeak = ref(0);

async function loadQps() {
  try {
    const from = new Date(Date.now() - 24 * 3600_000).toISOString();
    const d = await getLogQps({ from, intervalSec: 3600 });
    const pts = d.points ?? [];
    const counts = pts.map((p) => p.count);
    qpsTotal.value = counts.reduce((a, b) => a + b, 0);
    qps1h.value = counts.slice(-1)[0] ?? 0;
    qpsPeak.value = counts.length ? Math.max(...counts) : 0;
  } catch {
    /* QPS 数据不可用时保持 0 */
  }
}
onMounted(() => {
  load();
  void loadQps();
});

const ttlCols = [
  { title: '域名', dataIndex: 'domain' },
  { title: 'TTL（秒）', dataIndex: 'ttl', width: 120 },
];
</script>

<template>
  <div class="grid grid-cols-1 gap-4">
    <Card title="缓存参数">
      <div v-if="settings" class="flex flex-wrap items-center gap-6">
        <div>
          <div class="mb-1 text-xs text-gray-400">最小 TTL（秒）</div>
          <InputNumber v-model:value="settings.cacheMinTtl" :min="0" style="width: 130px" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">最大 TTL（秒）</div>
          <InputNumber v-model:value="settings.cacheMaxTtl" :min="1" style="width: 130px" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">serve-expired（过期旧值兜底）</div>
          <Switch v-model:checked="settings.serveExpired" />
        </div>
        <Button type="primary" :loading="saving" @click="save">保存并生效</Button>
      </div>
    </Card>

    <Card title="手动清空缓存">
      <div class="flex items-center gap-3">
        <Input v-model:value="flushZone" allow-clear placeholder="zone（留空=全部）" style="width: 240px" />
        <Button type="primary" danger @click="doFlush">清空缓存</Button>
        <span class="text-xs text-gray-400">unbound-control flush / flush_zone</span>
      </div>
    </Card>

    <Card title="每域名 TTL 覆盖（F-R3）">
      <div class="mb-3 flex items-center gap-2">
        <Input v-model:value="newTtl.domain" placeholder="域名 如 update.corp.local." style="width: 240px" />
        <InputNumber v-model:value="newTtl.ttl" :min="1" style="width: 120px" />
        <Button @click="addTtl">设置覆盖</Button>
      </div>
      <Table :data-source="ttlRows" :columns="ttlCols" row-key="domain" size="small" :pagination="false" />
      <div class="mt-3 flex flex-wrap gap-6 rounded border border-gray-200 p-3 text-sm">
        <span>最近 1 小时查询：<b>{{ qps1h }}</b></span>
        <span>近 24 小时总查询：<b>{{ qpsTotal }}</b></span>
        <span>小时峰值：<b>{{ qpsPeak }}</b></span>
      </div>
    </Card>
  </div>
</template>