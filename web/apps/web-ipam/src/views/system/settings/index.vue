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

function onLogoFile(e: Event) {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  if (file.size > 200 * 1024) {
    message.warning('图标请小于 200KB');
    input.value = '';
    return;
  }
  const reader = new FileReader();
  reader.onload = () => {
    const dataUrl = String(reader.result ?? '');
    form.faviconUrl = dataUrl;
    form.logoUrl = dataUrl;
    apply();
  };
  reader.readAsDataURL(file);
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
        <div class="mb-1 text-xs text-gray-400">站点图标（浏览器页签 + 侧栏共用，本地图片上传）</div>
        <div class="flex items-center gap-2">
          <img v-if="form.faviconUrl" :src="form.faviconUrl" alt="icon" class="h-8 w-8 rounded border border-gray-200 object-contain" />
          <input type="file" accept="image/png,image/jpeg,image/svg+xml,image/x-icon" @change="onLogoFile" />
          <Button v-if="form.faviconUrl" size="small" @click="form.faviconUrl = ''; form.logoUrl = ''">清除</Button>
        </div>
      </div>
      <div class="rounded border border-gray-200 p-3 text-xs text-gray-400">
        服务器：{{ server.ip || '—' }} : {{ server.port || '—' }}（只读——修改 IP/端口请改部署配置后重建容器）
      </div>
      <Button type="primary" :loading="saving" @click="save">保存并应用</Button>
    </div>
  </Card>
</template>
