<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Upload } from 'ant-design-vue';

import { requestClient } from '#/api/request';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { applyBranding } from '#/utils/site-branding';

defineOptions({ name: 'SystemBranding' });

const form = reactive({
  faviconUrl: '',
  logoUrl: '',
  serverPort: 8443,
  siteName: '',
});
const loading = ref(false);
const saving = ref(false);

function apply() {
  applyBranding(form);
}

async function load() {
  loading.value = true;
  try {
    const v = await requestClient.get('/system/webui-settings');
    const data = v as {
      faviconUrl?: string;
      logoUrl?: string;
      serverPort?: number;
      siteName?: string;
    };
    form.siteName = data.siteName ?? '';
    form.faviconUrl = data.faviconUrl ?? '';
    form.logoUrl = data.logoUrl ?? '';
    form.serverPort = Number(data.serverPort) || 8443;
    apply();
  } finally {
    loading.value = false;
  }
}

// adapter 增强 Upload：crop + 1:1 + 2MB，回调为裁剪后的 Blob
function onCropUpload(options: {
  file: Blob | File | string;
  onError?: (err: Error) => void;
  onSuccess?: (body?: unknown) => void;
}) {
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

function clearLogo() {
  form.faviconUrl = '';
  form.logoUrl = '';
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
      faviconUrl: form.faviconUrl,
      logoUrl: form.logoUrl,
      serverPort: form.serverPort,
      siteName: form.siteName,
    });
    apply();
    message.success('站点品牌已保存并应用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    saving.value = false;
  }
}

const previewName = computed(() => form.siteName.trim() || 'IPAM 管理平台');
const hasIcon = computed(() => Boolean(form.faviconUrl));

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:palette"
      title="站点品牌"
      description="配置浏览器页签与侧栏顶部的站点名称、图标；保存后立即应用，无需重启。"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存并应用
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="站点名称"
        :value="form.siteName || '未设置'"
        icon="lucide:type"
        tone="blue"
        hint="显示于浏览器页签、侧栏与登录页"
      />
      <StatCard
        label="站点图标"
        :value="hasIcon ? '已配置' : '默认图标'"
        icon="lucide:image"
        :tone="hasIcon ? 'green' : 'violet'"
        hint="1:1 裁剪，png / jpg / jpeg，≤2MB"
      />
      <StatCard
        label="生效方式"
        value="即时"
        icon="lucide:zap"
        tone="cyan"
        hint="保存后刷新页面即可看到品牌变化"
      />
      <StatCard
        label="服务端口"
        :value="form.serverPort"
        icon="lucide:plug"
        tone="amber"
        hint="端口在「服务与运行」中修改"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:panel-top" />品牌配置
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">图标与名称同时用于浏览器页签与侧栏顶部</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">站点名称</span>
          <Input v-model:value="form.siteName" placeholder="如 IPAM 管理平台" />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">站点图标（浏览器页签 + 侧栏 Logo，1:1 裁剪）</span>
          <div class="flex items-center gap-3">
            <Upload
              :custom-request="onCropUpload"
              :max-size="2"
              :show-upload-list="false"
              accept=".png,.jpg,.jpeg"
              aspect-ratio="1:1"
              crop="true"
              list-type="picture-card"
            >
              <div class="ipam-logo-upload">
                <img v-if="form.faviconUrl" :src="form.faviconUrl" alt="icon" />
                <span v-else class="text-2xl leading-none">+</span>
              </div>
            </Upload>
            <div class="ipam-hint">
              支持 png / jpg / jpeg，≤2MB，按 1:1 裁剪后保存
              <div class="mt-2">
                <Button v-if="form.faviconUrl" size="small" @click="clearLogo">
                  清除图标
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="ipam-brand-preview">
        <div class="ipam-brand-preview__tab">
          <span class="ipam-brand-preview__favicon">
            <img v-if="form.faviconUrl" :src="form.faviconUrl" alt="" />
            <IconifyIcon v-else icon="lucide:network" class="size-3" />
          </span>
          <span class="ipam-brand-preview__title">{{ previewName }}</span>
          <span class="ipam-brand-preview__dots"><i></i><i></i><i></i></span>
        </div>
        <div class="ipam-brand-preview__body">
          <div class="ipam-brand-preview__side">
            <span class="ipam-brand-preview__logo">
              <img v-if="form.faviconUrl" :src="form.faviconUrl" alt="" />
              <IconifyIcon v-else icon="lucide:network" class="size-4" />
            </span>
            <span class="ipam-brand-preview__item" data-active="true">概览</span>
            <span class="ipam-brand-preview__item">DHCP 服务</span>
            <span class="ipam-brand-preview__item">DNS 服务</span>
            <span class="ipam-brand-preview__item">日志中心</span>
          </div>
          <div class="ipam-brand-preview__canvas">
            <span class="ipam-brand-preview__hint">品牌实时预览</span>
          </div>
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">保存后立即应用；如需回退，重新上传旧图标或清除图标</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存并应用
        </Button>
      </div>
    </Card>
  </div>
</template>
