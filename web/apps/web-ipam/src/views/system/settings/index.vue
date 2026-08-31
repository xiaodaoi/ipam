<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { Button, Card, Input, message } from 'ant-design-vue';

import { requestClient } from '#/api/request';

const form = reactive({ siteName: '', faviconUrl: '', logoUrl: '' });
const server = reactive({ ip: '', port: '' });
const saving = ref(false);

function apply() {
  if (form.siteName) document.title = form.siteName;
  if (form.faviconUrl) {
    let link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = form.faviconUrl;
  }
}

async function load() {
  const v = await requestClient.get('/system/webui-settings');
  form.siteName = v.siteName ?? '';
  form.faviconUrl = v.faviconUrl ?? '';
  form.logoUrl = v.logoUrl ?? '';
  server.ip = v.serverIp ?? '';
  server.port = v.serverPort ?? '';
  apply();
}

async function save() {
  if (!form.siteName.trim()) {
    message.warning('站点名称必填');
    return;
  }
  saving.value = true;
  try {
    await requestClient.put('/system/webui-settings', {
      siteName: form.siteName,
      faviconUrl: form.faviconUrl,
      logoUrl: form.logoUrl,
    });
    apply();
    message.success('已保存并应用');
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <Card title="Web 页面设置">
    <div class="space-y-3">
      <div>
        <div class="mb-1 text-xs text-gray-400">站点名称（浏览器页签名称/侧栏显示）</div>
        <Input v-model:value="form.siteName" placeholder="如 IPAM 管理平台" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">浏览器页签 LOGO URL（favicon）</div>
        <Input v-model:value="form.faviconUrl" placeholder="https://.../favicon.ico" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">侧栏 LOGO URL</div>
        <Input v-model:value="form.logoUrl" placeholder="https://.../logo.png" />
      </div>
      <div class="rounded border border-gray-200 p-3 text-xs text-gray-400">
        服务器：{{ server.ip || '—' }} : {{ server.port || '—' }}（只读——修改 IP/端口请改部署配置后重建容器）
      </div>
      <Button type="primary" :loading="saving" @click="save">保存并应用</Button>
    </div>
  </Card>
</template>
