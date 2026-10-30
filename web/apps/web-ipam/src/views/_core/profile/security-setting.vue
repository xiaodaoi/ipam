<script setup lang="ts">
import { computed, ref } from 'vue';

import { useUserStore } from '@vben/stores';

import {
  Alert,
  Button,
  Card,
  Input,
  message,
  Popconfirm,
  Tag,
} from 'ant-design-vue';

import {
  disableTotpApi,
  enableTotpApi,
  logoutAllApi,
  setupTotpApi,
} from '#/api';
import { useAuthStore } from '#/store';

const userStore = useUserStore();
const authStore = useAuthStore();

const enabled = computed(() => Boolean(userStore.userInfo?.totpEnabled));
const setup = ref<null | { otpauthUrl: string; secret: string }>(null);
const code = ref('');
const password = ref('');
const loading = ref(false);

async function generate() {
  loading.value = true;
  try {
    setup.value = await setupTotpApi();
    message.success('已生成密钥，请用认证器扫描/录入后输入动态口令确认');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '生成失败');
  } finally {
    loading.value = false;
  }
}

async function enable() {
  if (!/^\d{6}$/.test(code.value.trim())) {
    message.warning('请输入 6 位动态口令');
    return;
  }
  loading.value = true;
  try {
    await enableTotpApi(code.value.trim());
    if (userStore.userInfo) {
      userStore.setUserInfo({ ...userStore.userInfo, totpEnabled: true });
    }
    setup.value = null;
    code.value = '';
    message.success('双因素认证已启用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '启用失败');
  } finally {
    loading.value = false;
  }
}

async function disable() {
  if (!password.value) {
    message.warning('请输入当前口令');
    return;
  }
  loading.value = true;
  try {
    await disableTotpApi(password.value);
    if (userStore.userInfo) {
      userStore.setUserInfo({ ...userStore.userInfo, totpEnabled: false });
    }
    password.value = '';
    message.success('双因素认证已关闭');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '关闭失败');
  } finally {
    loading.value = false;
  }
}

async function logoutAll() {
  try {
    await logoutAllApi();
    message.success('已注销全部会话，请重新登录');
    await authStore.logout();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <Card>
      <template #title>
        <span class="ipam-card-title">
          <span>双因素认证（TOTP）</span>
          <Tag class="m-0" :color="enabled ? 'green' : 'default'">
            {{ enabled ? '已启用' : '未启用' }}
          </Tag>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">兼容 Google Authenticator / Microsoft Authenticator / 1Password</span>
      </template>

      <Alert
        v-if="enabled"
        class="mb-3"
        message="登录时需要输入认证器生成的 6 位动态口令；关闭前需验证当前口令。"
        show-icon
        type="success"
      />
      <Alert
        v-else
        class="mb-3"
        message="启用后登录需输入动态口令，可显著降低口令泄露风险。密钥仅展示一次，请妥善保存。"
        show-icon
        type="info"
      />

      <template v-if="!enabled">
        <div v-if="!setup" class="flex items-center gap-3">
          <Button type="primary" :loading="loading" @click="generate">
            生成 TOTP 密钥
          </Button>
        </div>
        <div v-else class="ipam-form-grid">
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">密钥（手动录入认证器）</span>
            <Input :value="setup.secret" readonly class="ipam-mono" />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">otpauth URI</span>
            <Input :value="setup.otpauthUrl" readonly class="ipam-mono" />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">动态口令（6 位）</span>
            <Input
              v-model:value="code"
              :maxlength="6"
              placeholder="输入认证器当前口令"
              @press-enter="enable"
            />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <div class="flex gap-2">
              <Button type="primary" :loading="loading" @click="enable">
                确认启用
              </Button>
              <Button @click="setup = null">取消</Button>
            </div>
          </div>
        </div>
      </template>

      <div v-else class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">当前口令（关闭时验证）</span>
          <Input.Password
            v-model:value="password"
            placeholder="输入当前登录口令"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <Button danger :loading="loading" @click="disable">关闭双因素认证</Button>
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">会话安全</span>
      </template>
      <template #extra>
        <span class="ipam-hint">注销后所有设备需重新登录</span>
      </template>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="ipam-hint max-w-xl">
          退出全部会话会提升当前账号的令牌版本，所有设备上的访问/刷新令牌立即失效；
          当前页面也会回到登录页。
        </div>
        <Popconfirm
          title="确认注销全部会话？"
          description="所有设备将立即退出登录，需要重新认证。"
          ok-text="注销"
          cancel-text="取消"
          @confirm="logoutAll"
        >
          <Button danger>注销全部会话</Button>
        </Popconfirm>
      </div>
    </Card>
  </div>
</template>
