<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, message, Switch, Tag } from 'ant-design-vue';

import { getSecuritySettings, updateSecuritySettings } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

defineOptions({ name: 'SystemSecurity' });

const security = reactive({ forcePasswordChange: false });
const loading = ref(false);
const saving = ref(false);

async function load() {
  loading.value = true;
  try {
    const s = await getSecuritySettings();
    security.forcePasswordChange = Boolean(s.forcePasswordChange);
  } catch (error) {
    message.error(error instanceof Error ? error.message : '账号策略加载失败');
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
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
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:key-round"
      title="账号安全"
      description="集中管理口令策略、首次登录强制改密与登录保护；账号与角色在「账号管理」「角色管理」维护。"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存策略
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="强制首次改密"
        :value="security.forcePasswordChange ? '已开启' : '已关闭'"
        icon="lucide:shield-check"
        :tone="security.forcePasswordChange ? 'green' : 'violet'"
        :hint="security.forcePasswordChange ? '新建/重置账号首次登录须改密' : '便于调试；对外交付前建议开启'"
      />
      <StatCard
        label="口令最小长度"
        value="10"
        unit="位"
        icon="lucide:ruler"
        tone="blue"
        hint="最大 128 位；不含空白字符"
      />
      <StatCard
        label="字符类别"
        value="3 / 4"
        icon="lucide:case-sensitive"
        tone="cyan"
        hint="小写 / 大写 / 数字 / 符号"
      />
      <StatCard
        label="登录保护"
        value="5 次/分"
        icon="lucide:gauge"
        tone="amber"
        hint="按客户端 IP 限流；改密后旧会话立即失效"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />首次登录 / 重置口令后强制改密
        </span>
      </template>
      <template #extra>
        <Tag
          class="m-0"
          :color="security.forcePasswordChange ? 'green' : 'default'"
        >
          {{ security.forcePasswordChange ? '已启用' : '已关闭' }}
        </Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">策略开关</span>
          <div class="flex items-center gap-3">
            <Switch
              v-model:checked="security.forcePasswordChange"
              size="small"
            />
            <span class="ipam-hint">
              {{
                security.forcePasswordChange
                  ? '新建账号、管理员重置口令后，用户首次登录必须改密'
                  : '不强制改密；适合调试期，账号仍会记录“需改密”状态'
              }}
            </span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          开启后仅对之后的新建账号与重置口令生效，不会主动踢出已有会话；
          关闭不会清除已有“需改密”标记，只是登录时不再拦截。
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">对外交付前建议开启；调试期可关闭</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存策略
        </Button>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:list-checks" />固定口令策略
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">后端统一校验，创建/重置/自助改密共用</span>
      </template>
      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">长度</span>
          <div class="ipam-service-value"><span class="ipam-mono">10 – 128 位</span></div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">复杂度</span>
          <div class="ipam-service-value"><span>至少 3 类字符</span></div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">禁用内容</span>
          <div class="ipam-service-value"><span>用户名、空白、常见弱口令</span></div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">修改口令后</span>
          <div class="ipam-service-value"><span>其他设备会话立即失效</span></div>
        </div>
      </div>
    </Card>
  </div>
</template>
