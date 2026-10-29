<script setup lang="ts">
import { computed, reactive, ref } from 'vue';

import { useAccessStore, useUserStore } from '@vben/stores';

import { Alert, Button, Input, message, Modal } from 'ant-design-vue';

import { changePasswordApi } from '#/api';

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
    message.warning('请输入当前口令');
    return;
  }
  if (form.newPassword.length < 10) {
    message.warning('新口令至少 10 位');
    return;
  }
  if (form.newPassword !== form.confirmPassword) {
    message.warning('两次输入的新口令不一致');
    return;
  }
  if (form.newPassword === form.oldPassword) {
    message.warning('新口令不能与当前口令相同');
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
    message.success('口令已修改，请妥善保管新口令');
    reset();
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : '口令修改失败，请稍后重试',
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
    title="首次登录请修改口令"
    width="460px"
  >
    <Alert
      class="mb-4"
      message="为保障账号安全，首次登录或管理员重置口令后必须修改口令后才能继续使用。"
      show-icon
      type="warning"
    />
    <div class="flex flex-col gap-3">
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">当前口令</span>
        <Input.Password
          v-model:value="form.oldPassword"
          autocomplete="current-password"
          placeholder="请输入当前口令"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">新口令（≥10 位，至少 3 类字符）</span>
        <Input.Password
          v-model:value="form.newPassword"
          autocomplete="new-password"
          placeholder="小写/大写/数字/符号至少 3 类"
        />
      </label>
      <label class="ipam-form-field">
        <span class="ipam-form-field__label">确认新口令</span>
        <Input.Password
          v-model:value="form.confirmPassword"
          autocomplete="new-password"
          placeholder="再次输入新口令"
          @press-enter="submit"
        />
      </label>
      <div class="ipam-form-hint">
        修改成功后，其他设备上的登录会话将立即失效，需要使用新口令重新登录。
      </div>
      <div class="flex justify-end">
        <Button type="primary" :loading="loading" @click="submit">
          确认修改
        </Button>
      </div>
    </div>
  </Modal>
</template>
