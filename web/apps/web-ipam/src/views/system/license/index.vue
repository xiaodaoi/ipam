<script setup lang="ts">
import type { LicenseStatus } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Popconfirm, Tag } from 'ant-design-vue';

import {
  activateLicense,
  getLicenseStatus,
  removeLicense,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

defineOptions({ name: 'SystemLicense' });

const status = ref<LicenseStatus>();
const text = ref('');
const loading = ref(false);
const activating = ref(false);
const removing = ref(false);

const EDITION_LABEL: Record<string, string> = {
  community: '社区版',
  enterprise: '企业版',
  lite: '轻量版',
  standard: '标准版',
};

const licensed = computed(() => Boolean(status.value?.licensed));
const edition = computed(() => EDITION_LABEL[status.value?.edition ?? 'community'] ?? '社区版');
const expiry = computed(() => {
  if (!status.value?.expiresAt) return '永久';
  const t = Date.parse(status.value.expiresAt);
  if (Number.isNaN(t)) return '—';
  return new Date(t).toLocaleDateString('zh-CN');
});
const terminal = computed(() => {
  const n = status.value?.maxTerminals ?? 0;
  return n > 0 ? `${n} 台` : '不限';
});
const days = computed(() => {
  const n = status.value?.daysRemaining ?? -1;
  if (!status.value?.expiresAt) return '永久';
  return n >= 0 ? `${n} 天` : '已过期';
});

async function load() {
  loading.value = true;
  try {
    status.value = await getLicenseStatus();
    if (status.value?.licensed) {
      text.value = '';
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : 'License 状态加载失败');
  } finally {
    loading.value = false;
  }
}

async function activate() {
  if (!text.value.trim()) {
    message.warning('请粘贴 License 文件内容');
    return;
  }
  activating.value = true;
  try {
    status.value = await activateLicense(text.value.trim());
    text.value = '';
    message.success('License 已激活');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '激活失败');
  } finally {
    activating.value = false;
  }
}

async function remove() {
  removing.value = true;
  try {
    await removeLicense();
    await load();
    message.success('License 已移除，当前为社区版');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '移除失败');
  } finally {
    removing.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:badge-check"
      title="License"
      description="离线授权文件管理：Ed25519 签名校验，激活后展示版本、能力与到期时间；未激活为社区版。"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Popconfirm
          title="确认移除 License？"
          description="移除后回到社区版，已授权能力可能立即失效。"
          ok-text="移除"
          cancel-text="取消"
          @confirm="remove"
        >
          <Button size="small" danger :loading="removing" :disabled="!licensed">
            移除 License
          </Button>
        </Popconfirm>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="当前版本"
        :value="edition"
        icon="lucide:award"
        :tone="licensed ? 'green' : 'violet'"
        :hint="licensed ? '已激活' : '未激活 · 社区版'"
      />
      <StatCard
        label="授权状态"
        :value="licensed ? '有效' : '未授权'"
        icon="lucide:shield-check"
        :tone="licensed ? 'green' : 'amber'"
        :hint="status?.licenseId || '请激活 License 后使用商业能力'"
      />
      <StatCard
        label="到期时间"
        :value="expiry"
        icon="lucide:calendar-clock"
        tone="cyan"
        :hint="days"
      />
      <StatCard
        label="终端上限"
        :value="terminal"
        icon="lucide:monitor-smartphone"
        tone="blue"
        hint="按授权文件约定；超限后续做软降级"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:badge-check" />授权信息
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="licensed ? 'green' : 'default'">
          {{ licensed ? '已激活' : '社区版' }}
        </Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">授权客户</span>
          <div class="ipam-service-value">{{ status?.issuedTo || '—' }}</div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">License ID</span>
          <div class="ipam-service-value ipam-mono">{{ status?.licenseId || '—' }}</div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">授权能力</span>
          <div class="flex flex-wrap gap-1">
            <Tag v-for="f in status?.features ?? []" :key="f" class="m-0">{{ f }}</Tag>
            <span v-if="(status?.features ?? []).length === 0" class="ipam-hint">—</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">授权文件路径</span>
          <div class="ipam-service-value ipam-mono">{{ status?.filePath || '—' }}</div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          {{ status?.message || '未激活' }}
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:file-key" />激活 License
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">需要服务端配置 IPAM_LICENSE_PUBKEY</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">License 文件内容（JSON）</span>
          <Input.TextArea
            v-model:value="text"
            :rows="8"
            class="ipam-mono"
            placeholder="payload / signature JSON"
          />
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          激活会先校验签名与有效期，失败不会覆盖现有授权；文件写入后立即生效。
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">生产环境请通过正式渠道获取 License；私钥由授权方保管</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="activating" @click="activate">
          <IconifyIcon icon="lucide:check" class="mr-1 size-3.5" />激活
        </Button>
      </div>
    </Card>
  </div>
</template>
