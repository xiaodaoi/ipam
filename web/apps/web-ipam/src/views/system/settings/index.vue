<script setup lang="ts">
import type { OrgTreeNode, SystemInfo } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Switch,
  Tag,
  Upload,
} from 'ant-design-vue';

import {
  getLogSettings,
  getSecuritySettings,
  getSystemInfo,
  listOrgTree,
  updateSecuritySettings,
} from '#/api/ipam';
import { requestClient } from '#/api/request';
import LogStoragePanel from '#/components/log-storage-panel.vue';
import OrgManageCard from '#/components/org-manage-card.vue';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';
import { applyBranding } from '#/utils/site-branding';

const form = reactive({ faviconUrl: '', logoUrl: '', serverPort: 8443, siteName: '' });
const restarting = ref(false);
const saving = ref(false);
const loading = ref(false);
const orgCount = ref(0);
const retentionDays = ref<number>();
const info = ref<SystemInfo>();
const security = reactive({ forcePasswordChange: false });
const savingSecurity = ref(false);

const mode = import.meta.env.MODE === 'production' ? '生产环境' : '开发环境';
const modeTone = import.meta.env.MODE === 'production' ? 'green' : 'amber';
const modeTagColor = import.meta.env.MODE === 'production' ? 'green' : 'orange';

// 侧栏 logo 渲染：preferences.logo 支持 URL（以 http/data 开头按图片渲染）
function apply() {
  applyBranding(form);
}

function countOrgs(nodes: OrgTreeNode[]): number {
  return nodes.reduce(
    (sum, n) => sum + 1 + countOrgs((n.children ?? []) as OrgTreeNode[]),
    0,
  );
}

async function load() {
  loading.value = true;
  const [web, orgs, logSettings, sysInfo, sec] = await Promise.allSettled([
    requestClient.get('/system/webui-settings'),
    listOrgTree(),
    getLogSettings(),
    getSystemInfo(),
    getSecuritySettings(),
  ]);
  if (web.status === 'fulfilled') {
    const v = web.value as {
      faviconUrl?: string;
      logoUrl?: string;
      serverPort?: number;
      siteName?: string;
    };
    form.siteName = v.siteName ?? '';
    form.faviconUrl = v.faviconUrl ?? '';
    form.logoUrl = v.logoUrl ?? '';
    form.serverPort = Number(v.serverPort) || 8443;
    apply();
  }
  orgCount.value = orgs.status === 'fulfilled' ? countOrgs(orgs.value ?? []) : 0;
  retentionDays.value =
    logSettings.status === 'fulfilled' ? logSettings.value.retentionDays : undefined;
  info.value = sysInfo.status === 'fulfilled' ? sysInfo.value : undefined;
  security.forcePasswordChange =
    sec.status === 'fulfilled' ? Boolean(sec.value.forcePasswordChange) : false;
  loading.value = false;
}

// 裁剪上传（adapter 增强 Upload：crop="true" + aspect-ratio="1:1" + max-size=2MB，
// 选中图片后自动弹 VCropper 裁剪，此处拿到 1:1 裁剪后的 Blob）
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
  if (form.serverPort < 1 || form.serverPort > 65_535) {
    message.warning('端口需在 1-65535 之间');
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

async function saveSecurity() {
  savingSecurity.value = true;
  try {
    const saved = await updateSecuritySettings(security.forcePasswordChange);
    security.forcePasswordChange = Boolean(saved.forcePasswordChange);
    message.success(
      saved.forcePasswordChange
        ? '已开启：新建账号与重置口令后首次登录须改密'
        : '已关闭：首次登录不再强制改密（便于调试）',
    );
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    savingSecurity.value = false;
  }
}

const previewName = computed(() => form.siteName.trim() || 'IPAM 管理平台');

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:settings"
      title="系统设置"
      description="维护组织架构、站点品牌与服务运行参数；日志保留策略与归档统一在「日志中心 → 存储与归档」管理。"
    >
      <template #actions>
        <RouterLink to="/logs-center/storage">
          <Button size="small">
            <IconifyIcon icon="lucide:database" class="mr-1 size-3.5" />日志存储与归档
          </Button>
        </RouterLink>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="服务端口"
        :value="form.serverPort"
        icon="lucide:plug"
        tone="blue"
        hint="修改后需重启控制面生效"
      />
      <StatCard
        label="组织节点"
        :value="orgCount"
        icon="lucide:git-branch"
        tone="cyan"
        hint="组织树全部层级节点合计"
      />
      <StatCard
        label="日志保留"
        :value="retentionDays === undefined ? '—' : retentionDays"
        unit="天"
        icon="lucide:timer-reset"
        tone="violet"
        :hint="retentionDays === undefined ? '日志策略暂不可达' : 'ClickHouse TTL 自动滚动删除'"
      />
      <StatCard
        label="运行模式"
        :value="mode"
        icon="lucide:server-cog"
        :tone="modeTone"
        :hint="info?.version ? `控制面 ${info.version} · ${info.goVersion ?? ''}` : '控制面版本暂不可达'"
      />
    </div>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <OrgManageCard />

      <Card>
        <template #title>
          <span class="ipam-card-title">
            <IconifyIcon icon="lucide:panel-top" />站点品牌
          </span>
        </template>
        <template #extra>
          <span class="ipam-hint">保存后立即应用</span>
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
          <span class="ipam-hint">图标与名称同时用于浏览器页签与侧栏顶部</span>
          <span class="ipam-toolbar__spacer"></span>
          <Button type="primary" :loading="saving" @click="save">
            <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存并应用
          </Button>
        </div>
      </Card>
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />账号安全
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">仅影响新建账号与后续重置，不踢出已有会话</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">首次登录 / 重置口令后强制改密</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="security.forcePasswordChange" size="small" />
            <span class="ipam-hint">
              {{ security.forcePasswordChange ? '已启用' : '已关闭（便于调试）' }}
            </span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">口令策略（固定）</span>
          <div class="ipam-service-value">
            <span>≥10 位 · 至少 3 类字符 · 不含用户名</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          关闭后，新建账号/重置口令仍会记录“需改密”状态，但不会强制弹窗；开启后仅对之后的新建/重置立即生效。
        </div>
      </div>
      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">调试期建议关闭；对外交付前建议开启</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="savingSecurity" @click="saveSecurity">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存账号策略
        </Button>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:plug-zap" />服务与运行
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="modeTagColor">{{ mode }}</Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">服务端口</span>
          <InputNumber
            v-model:value="form.serverPort"
            :max="65_535"
            :min="1"
            style="width: 100%"
          />
          <span class="ipam-form-hint">容器部署请同步 `.env` 的 IPAM_PORT 并重建容器</span>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">控制面版本</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">{{ info?.version ?? '—' }}</span>
            <span class="ipam-hint">{{ info?.goVersion ?? '' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">服务端时间</span>
          <div class="ipam-service-value">
            <span class="ipam-mono">
              {{ info?.now ? new Date(info.now).toLocaleString('zh-CN', { hour12: false }) : '—' }}
            </span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">依赖就绪</span>
          <div class="ipam-service-value">
            <Tag class="m-0" :color="info?.ready ? 'green' : 'red'">
              {{ info?.ready ? '正常' : '异常 / 未接入' }}
            </Tag>
          </div>
        </div>
      </div>

      <div class="ipam-danger-zone">
        <div class="ipam-danger-zone__body">
          <div class="ipam-danger-zone__title">
            <IconifyIcon icon="lucide:triangle-alert" class="size-4" />重启控制面服务
          </div>
          <div class="ipam-danger-zone__desc">
            重启期间管理页面与 API 会短暂不可用（约 5–15 秒），DHCP/DNS 数据面不受影响；
            端口等配置修改在重启后生效。
          </div>
        </div>
        <Popconfirm
          title="确认重启服务？"
          description="重启后页面将短暂不可用；端口/设置修改在重启后生效。"
          ok-text="重启"
          cancel-text="取消"
          @confirm="restart"
        >
          <Button danger :loading="restarting">
            <IconifyIcon icon="lucide:power" class="mr-1 size-3.5" />重启服务
          </Button>
        </Popconfirm>
      </div>
    </Card>

    <LogStoragePanel />
  </div>
</template>

<style scoped>
/* 本页各区块统一由 flex gap 控制纵向间距；抵消 ant Card 自带的 margin-bottom，
   否则会与 gap 叠加造成行间距不一致（12+16=28px vs 16px）。 */
:deep(.ant-card) {
  margin-bottom: 0;
}
</style>
