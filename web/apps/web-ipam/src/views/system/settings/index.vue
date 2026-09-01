<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { updatePreferences } from '@vben/preferences';

import { Button, Card, Input, InputNumber, Popconfirm, message } from 'ant-design-vue';

import { requestClient } from '#/api/request';

const form = reactive({ siteName: '', faviconUrl: '', logoUrl: '', serverPort: 8443 });
const restarting = ref(false);
const saving = ref(false);

// 侧栏 logo 渲染：preferences.logo 支持 URL（以 http/data 开头按图片渲染）
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
  // 同步侧栏：站点名称 + logo（页签/侧栏共用）
  updatePreferences({
    app: { name: form.siteName },
    logo: { source: form.logoUrl },
  });
}

async function load() {
  const v = await requestClient.get('/system/webui-settings');
  form.siteName = v.siteName ?? '';
  form.faviconUrl = v.faviconUrl ?? '';
  form.logoUrl = v.logoUrl ?? '';
  form.serverPort = Number(v.serverPort) || 8443;
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
  if (form.serverPort < 1 || form.serverPort > 65535) {
    message.warning('端口需在 1-65535 之间');
    return;
  }
  saving.value = true;
  try {
    await requestClient.put('/system/webui-settings', {
      siteName: form.siteName,
      faviconUrl: form.faviconUrl,
      logoUrl: form.logoUrl,
      serverPort: form.serverPort,
    });
    apply();
    message.success('已保存并应用（端口修改需重启后生效）');
  } finally {
    saving.value = false;
  }
}

async function restart() {
  restarting.value = true;
  try {
    await requestClient.post('/system/restart');
    message.success('已发起重启，页面将短暂不可用，请稍后刷新');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '重启请求失败');
  } finally {
    restarting.value = false;
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
      <div class="flex items-center gap-2">
        <div class="mb-1 text-xs text-gray-400">服务端口</div>
        <InputNumber v-model:value="form.serverPort" :min="1" :max="65535" style="width: 160px" />
        <span class="text-xs text-gray-400">修改后需重启生效（容器部署请同步 .env 的 IPAM_PORT 并重建容器）</span>
      </div>
      <div class="flex items-center gap-2">
        <Button type="primary" :loading="saving" @click="save">保存并应用</Button>
        <Popconfirm
          title="确认重启服务？"
          description="重启后页面将短暂不可用；端口/设置修改在重启后生效。"
          ok-text="重启"
          cancel-text="取消"
          @confirm="restart"
        >
          <Button danger :loading="restarting">重启服务</Button>
        </Popconfirm>
      </div>
    </div>
  </Card>
</template>