<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { Button, Card, Input, InputNumber, message, Select, Switch, Table, Tag } from 'ant-design-vue';

import {
  diagnoseDns,
  type DiagnoseRequestRow,
  getDnsSettings,
  updateDnsSettings,
  type DiagnoseResult,
  type DnsSettings,
} from '#/api/ipam';

const settings = ref<DnsSettings>();
const testName = ref('');
const testType = ref<DiagnoseRequestRow['type']>('A');
const testClientIp = ref('');
const testing = ref(false);
const diag = ref<DiagnoseResult>();
const saving = ref(false);

async function load() {
  settings.value = await getDnsSettings();
}
async function save() {
  if (!settings.value) return;
  saving.value = true;
  try {
    settings.value = await updateDnsSettings(settings.value);
    message.success('安全参数已保存并生效');
  } finally {
    saving.value = false;
  }
}
const QTYPE_OPTIONS = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'PTR', 'SOA'].map(
  (t) => ({ label: t, value: t }),
);
const RCODE_COLOR: Record<string, string> = { NOERROR: 'green', NXDOMAIN: 'orange' };

async function runTest() {
  if (!testName.value) return;
  testing.value = true;
  try {
    diag.value = await diagnoseDns({ name: testName.value, type: testType.value, clientIp: testClientIp.value || undefined });
  } catch (e) {
    message.error(e instanceof Error ? e.message : '查询失败');
  } finally {
    testing.value = false;
  }
}
onMounted(load);
</script>

<template>
  <div class="grid grid-cols-1 gap-4">
    <Card title="应答限速 RRL（防放大 B-08）">
      <div v-if="settings" class="flex flex-wrap items-center gap-6">
        <div class="flex items-center gap-2">
          <span>启用限速</span>
          <Switch v-model:checked="settings.rrlEnabled" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">ip-ratelimit（次/秒）</div>
          <InputNumber
            v-model:value="settings.rrlRate"
            :min="10"
            :disabled="!settings.rrlEnabled"
            style="width: 140px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span>DNSSEC 校验（B-10）</span>
          <Switch v-model:checked="settings.dnssecValidate" />
        </div>
        <Button type="primary" :loading="saving" @click="save">保存并生效</Button>
      </div>
      <div class="mt-2 text-xs text-gray-400">
        限速超限的来源 IP 将被丢弃应答（ip-ratelimit 按 unbound 官方定义）。
      </div>
    </Card>

    <Card title="解析测试台（dig 式，经 unbound 实时查询）">
      <div class="flex flex-wrap items-center gap-3">
        <Input
          v-model:value="testName"
          placeholder="如 example.com；PTR 直接填 192.168.0.10"
          style="width: 320px"
          @press-enter="runTest"
        />
        <Select v-model:value="testType" style="width: 100px" :options="QTYPE_OPTIONS" />
        <Input v-model:value="testClientIp" placeholder="模拟来源 IP（可选，提示命中 view）" style="width: 220px" />
        <Button type="primary" :loading="testing" @click="runTest">查询</Button>
      </div>
      <div v-if="diag" class="mt-3">
        <div class="flex items-center gap-3">
          <Tag :color="RCODE_COLOR[diag.rcode] ?? 'red'">{{ diag.rcode }}</Tag>
          <span class="text-xs text-gray-400">
            {{ diag.rttMs }}ms · {{ diag.server }} · {{ diag.answers.length }} 条答案
            <Tag v-if="diag.viewHint" color="purple">view: {{ diag.viewHint }}</Tag>
          </span>
        </div>
        <Table
          v-if="diag.answers.length"
          class="mt-2"
          :data-source="diag.answers.map((a, i) => ({ ...a, key: i }))"
          :columns="[
            { title: '记录名', dataIndex: 'name' },
            { title: '类型', dataIndex: 'type', width: 90 },
            { title: 'TTL', dataIndex: 'ttl', width: 90 },
            { title: '值', dataIndex: 'value' },
          ]"
          size="small"
          :pagination="false"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'ttl'">{{ record.ttl ?? '-' }}</template>
          </template>
        </Table>
      </div>
    </Card>
  </div>
</template>