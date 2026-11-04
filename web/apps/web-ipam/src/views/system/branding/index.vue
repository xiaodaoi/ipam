<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Upload } from 'ant-design-vue';

import { requestClient } from '#/api/request';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { $t } from '#/locales';
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
    options.onError?.(new Error($t('system.branding.msg.readImageFailed'))),
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
    message.warning($t('system.branding.msg.nameRequired'));
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
    message.success($t('system.branding.msg.saved'));
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('system.branding.msg.saveFailed'),
    );
  } finally {
    saving.value = false;
  }
}

const previewName = computed(
  () =>
    form.siteName.trim() || $t('system.branding.defaultSiteName'),
);
const hasIcon = computed(() => Boolean(form.faviconUrl));

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:palette"
      :title="$t('system.branding.page.title')"
      :description="$t('system.branding.page.desc')"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />{{
            $t('system.branding.actions.refresh')
          }}
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.branding.actions.save')
          }}
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        :label="$t('system.branding.stat.name')"
        :value="form.siteName || $t('system.branding.stat.unset')"
        icon="lucide:type"
        tone="blue"
        :hint="$t('system.branding.stat.nameHint')"
      />
      <StatCard
        :label="$t('system.branding.stat.icon')"
        :value="
          hasIcon
            ? $t('system.branding.stat.configured')
            : $t('system.branding.stat.defaultIcon')
        "
        icon="lucide:image"
        :tone="hasIcon ? 'green' : 'violet'"
        :hint="$t('system.branding.stat.iconHint')"
      />
      <StatCard
        :label="$t('system.branding.stat.apply')"
        :value="$t('system.branding.stat.applyValue')"
        icon="lucide:zap"
        tone="cyan"
        :hint="$t('system.branding.stat.applyHint')"
      />
      <StatCard
        :label="$t('system.branding.stat.port')"
        :value="form.serverPort"
        icon="lucide:plug"
        tone="amber"
        :hint="$t('system.branding.stat.portHint')"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:panel-top" />{{
            $t('system.branding.card.title')
          }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('system.branding.card.hint') }}</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.branding.card.siteName')
          }}</span>
          <Input
            v-model:value="form.siteName"
            :placeholder="$t('system.branding.card.siteNamePlaceholder')"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">{{
            $t('system.branding.card.iconLabel')
          }}</span>
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
              {{ $t('system.branding.card.uploadHint') }}
              <div class="mt-2">
                <Button v-if="form.faviconUrl" size="small" @click="clearLogo">
                  {{ $t('system.branding.card.clear') }}
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
            <span class="ipam-brand-preview__item" data-active="true">{{
              $t('system.branding.preview.overview')
            }}</span>
            <span class="ipam-brand-preview__item">{{
              $t('system.branding.preview.dhcp')
            }}</span>
            <span class="ipam-brand-preview__item">{{
              $t('system.branding.preview.dns')
            }}</span>
            <span class="ipam-brand-preview__item">{{
              $t('system.branding.preview.logs')
            }}</span>
          </div>
          <div class="ipam-brand-preview__canvas">
            <span class="ipam-brand-preview__hint">{{
              $t('system.branding.card.previewHint')
            }}</span>
          </div>
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">{{ $t('system.branding.footerHint') }}</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />{{
            $t('system.branding.actions.save')
          }}
        </Button>
      </div>
    </Card>
  </div>
</template>
