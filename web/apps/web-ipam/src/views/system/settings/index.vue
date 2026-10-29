<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Popconfirm, Upload } from 'ant-design-vue';

import { requestClient } from '#/api/request';
import LogStoragePanel from '#/components/log-storage-panel.vue';
import OrgManageCard from '#/components/org-manage-card.vue';
import PageHeader from '#/components/page-header.vue';
import { applyBranding } from '#/utils/site-branding';

const form = reactive({ siteName: '', faviconUrl: '', logoUrl: '', serverPort: 8443 });
const restarting = ref(false);
const saving = ref(false);

// 侧栏 logo 渲染：preferences.logo 支持 URL（以 http/data 开头按图片渲染）
function apply() {
  applyBranding(form);
}

async function load() {
  const v = await requestClient.get('/system/webui-settings');
  form.siteName = v.siteName ?? '';
  form.faviconUrl = v.faviconUrl ?? '';
  form.logoUrl = v.logoUrl ?? '';
  form.serverPort = Number(v.serverPort) || 8443;
  apply();
}

// 裁剪上传（adapter 增强 Upload：crop="true" + aspect-ratio="1:1" + max-size=2MB，
// 选中图片后自动弹 VCropper 裁剪，此处拿到 1:1 裁剪后的 Blob）
function onCropUpload(options: { file: Blob | File | string; onError?: (err: Error) => void; onSuccess?: (body?: unknown) => void; }) {
  const reader = new FileReader();
  reader.addEventListener('load', () => {
    form.faviconUrl = String(reader.result);
    form.logoUrl = form.faviconUrl;
    apply();
    options.onSuccess?.(options.file);
  });
  reader.addEventListener('error', () =>
    options.onError?.(new Error('读取图片失败')),
  );
  reader.readAsDataURL(options.file as Blob);
}

async function save() {
  if (!form.siteName.trim()) {
    message.warning('站点名称必填');
    return;
  }
  if (form.serverPort < 1 || form.serverPort > 65_535) {
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
  } catch (error) {
    message.error(error instanceof Error ? error.message : '重启请求失败');
  } finally {
    restarting.value = false;
  }
}

onMounted(() => {
  load();
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:settings"
      title="系统设置"
      description="维护组织架构与站点品牌；日志保留策略与归档统一在「日志中心 → 存储与归档」管理，品牌配置保存后即时生效。"
    >
      <template #actions>
        <RouterLink to="/logs-center/storage">
          <Button size="small">
            <IconifyIcon icon="lucide:database" class="mr-1 size-3.5" />日志存储与归档
          </Button>
        </RouterLink>
        <Button size="small" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="flex flex-col gap-4">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start">
        <div class="min-w-0 flex-1">
          <OrgManageCard />
        </div>
        <div class="min-w-0 flex-1">
          <Card title="Web 页面设置">
            <template #extra>
              <span class="ipam-hint">保存后立即应用到页签与侧栏</span>
            </template>
            <div class="space-y-3">
              <div>
                <div class="mb-1 text-xs text-gray-400">站点名称（浏览器页签名称/侧栏显示）</div>
                <Input v-model:value="form.siteName" placeholder="如 IPAM 管理平台" />
              </div>
              <div>
                <div class="mb-1 text-xs text-gray-400">站点图标（浏览器页签 + 侧栏共用，1:1 裁剪上传）</div>
                <div class="flex items-center gap-3">
                  <Upload
                    :show-upload-list="false"
                    accept=".png,.jpg,.jpeg"
                    :max-size="2"
                    crop="true"
                    aspect-ratio="1:1"
                    list-type="picture-card"
                    :custom-request="onCropUpload"
                  >
                    <div
                      class="flex h-24 w-24 flex-col items-center justify-center overflow-hidden rounded border border-dashed border-gray-300 text-gray-400 transition-colors hover:border-primary hover:text-primary"
                    >
                      <img v-if="form.faviconUrl" :src="form.faviconUrl" alt="icon" class="h-full w-full object-contain" />
                      <span v-else class="text-2xl leading-none">+</span>
                    </div>
                  </Upload>
                  <div class="text-xs text-gray-400">
                    支持 png / jpg / jpeg，≤2MB，按 1:1 裁剪后保存
                    <Button v-if="form.faviconUrl" size="small" class="ml-2" @click="form.faviconUrl = ''; form.logoUrl = ''">
                      清除
                    </Button>
                  </div>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <div class="mb-1 flex-none whitespace-nowrap text-xs text-gray-400">服务端口</div>
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
        </div>
      </div>

      <LogStoragePanel />
    </div>
  </div>
</template>

<style scoped>
/* 本页各区块统一由 flex gap 控制纵向间距；抵消 ant Card 自带的 margin-bottom，
   否则会与 gap 叠加造成行间距不一致（12+16=28px vs 16px）。 */
:deep(.ant-card) {
  margin-bottom: 0;
}
</style>