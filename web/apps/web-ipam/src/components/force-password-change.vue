<script setup lang="ts">
import { computed, reactive, ref } from 'vue';

import { useAccessStore, useUserStore } from '@vben/stores';

import { Alert, Button, Input, message, Modal } from 'ant-design-vue';

import { changePasswordApi } from '#/api';
import { $t } from '#/locales';

const userStore = useUserStore();
const accessStore = useAccessStore();

const loading = ref(false);
const form = reactive({
  confirmPassword: '',
  newPassword: '',
  oldPassword: '',
});

// 首次登录 / 管理员重置后：后端返回 mustChangePassword=true，前端强制改密。
// 弹窗不可关闭、不可点遮罩、不可 Esc；改密成功后才放行。
const open = computed(() => Boolean(userStore.userInfo?.mustChangePassword));

function reset() {
  form.oldPassword = '';
  form.newPassword = '';
  form.confirmPassword = '';
}

async function submit() {
  if (!form.oldPassword) {
    message.warning($t('shared.passwordChange.warnOldRequired'));
    return;
  }
  if (form.newPassword.length < 10) {
    message.warning($t('shared.passwordChange.warnMinLength'));
    return;
  }
  if (form.newPassword !== form.confirmPassword) {
    message.warning($t('shared.passwordChange.warnMismatch'));
    return;
  }
  if (form.newPassword === form.oldPassword) {
    message.warning($t('shared.passwordChange.warnSame'));
    return;
  }
  loading.value = true;
  try {
    const result = await changePasswordApi({
      newPassword: form.newPassword,
      oldPassword: form.oldPassword,
    });
    if (result.accessToken) {
      accessStore.setAccessToken(result.accessToken);
    }
    if (userStore.userInfo) {
      userStore.setUserInfo({
        ...userStore.userInfo,
        mustChangePassword: false,
      });
    }
    message.success($t('shared.passwordChange.success'));
    reset();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : $t('shared.passwordChange.failed'),
    );
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <Modal
    :closable="false"
    :footer="null"
    :keyboard="false"
    :mask-closable="false"
    :open="open"
    :title="$t('shared.passwordChange.title')"
    width="460px"
  >
    <Alert
      class="mb-4"
      :message="$t('shared.passwordChange.alert')"
      show-icon
      type="warning"
    />
    <div class="flex flex-col gap-3">
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{ $t('shared.passwordChange.oldLabel') }}</span>
        <Input.Password
          v-model:value="form.oldPassword"
          autocomplete="current-password"
          :placeholder="$t('shared.passwordChange.oldPlaceholder')"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{ $t('shared.passwordChange.newLabel') }}</span>
        <Input.Password
          v-model:value="form.newPassword"
          autocomplete="new-password"
          :placeholder="$t('shared.passwordChange.newPlaceholder')"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">{{ $t('shared.passwordChange.confirmLabel') }}</span>
        <Input.Password
          v-model:value="form.confirmPassword"
          autocomplete="new-password"
          :placeholder="$t('shared.passwordChange.confirmPlaceholder')"
          @press-enter="submit"
        />
      </label>
      <div class="ipam-form-hint">
        {{ $t('shared.passwordChange.hint') }}
      </div>
      <div class="flex justify-end">
        <Button type="primary" :loading="loading" @click="submit">
          {{ $t('shared.passwordChange.submit') }}
        </Button>
      </div>
    </div>
  </Modal>
</template>
