<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { Card, Input, InputNumber, message, Switch } from 'ant-design-vue';

import { getDnsSettings, updateDnsSettings, type DnsSettings } from '#/api/ipam';

const settings = ref<DnsSettings>();
const testQuery = ref('');
const testResult = ref<string>();
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
function runTest() {
  // 解析测试台 P1（需 unbound dig 容器联动）；当前给出指引占位
  testResult.value =
    '解析测试台将在后续版本提供（需选定 view 并从指定来源发起 dig 式查询）。' +
    '当前可在服务器上执行：dig @127.0.0.1 ' +
    (testQuery.value || 'example.com') + ' +short';
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
          <div class="mb-1 text-xs text-gray-400">ratelimit-per-ip（次/秒）</div>
          <InputNumber
            v-model:value="settings.rrlRate"
            :min="10"
            :disabled="!settings.rrlEnabled"
            style="width: 140px"
          />
        </div>
        <div class="flex items-center gap-2">
          <span>DNSSEC 校验（P2）</span>
          <Switch v-model:checked="settings.dnssecValidate" disabled />
        </div>
        <Button type="primary" :loading="saving" @click="save">保存并生效</Button>
      </div>
      <div class="mt-2 text-xs text-gray-400">
        限速超限的来源 IP 将被丢弃应答（ratelimit 语义按 unbound 官方定义）。
      </div>
    </Card>

    <Card title="解析测试台（占位）">
      <div class="flex items-center gap-3">
        <Input v-model:value="testQuery" placeholder="如 api.corp.local" style="width: 260px" />
        <Button @click="runTest">发起测试</Button>
      </div>
      <div v-if="testResult" class="mt-3 rounded bg-gray-50 p-3 font-mono text-xs text-gray-600">
        {{ testResult }}
      </div>
    </Card>
  </div>
</template>