<script setup lang="ts">
import type { VbenFormSchema } from '#/adapter/form';

import { computed } from 'vue';

import { ProfilePasswordSetting, z } from '@vben/common-ui';
import { useAccessStore, useUserStore } from '@vben/stores';

import { message } from 'ant-design-vue';

import { changePasswordApi } from '#/api';

const accessStore = useAccessStore();
const userStore = useUserStore();

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      fieldName: 'oldPassword',
      label: '旧密码',
      component: 'VbenInputPassword',
      componentProps: {
        placeholder: '请输入旧密码',
      },
      rules: z.string().min(1, { message: '请输入旧密码' }),
    },
    {
      fieldName: 'newPassword',
      label: '新密码',
      component: 'VbenInputPassword',
      componentProps: {
        passwordStrength: true,
        placeholder: '≥10 位，至少 3 类字符',
      },
      rules: z
        .string()
        .min(10, { message: '新密码至少 10 位' })
        .max(128, { message: '新密码最长 128 位' }),
    },
    {
      fieldName: 'confirmPassword',
      label: '确认密码',
      component: 'VbenInputPassword',
      componentProps: {
        passwordStrength: true,
        placeholder: '请再次输入新密码',
      },
      dependencies: {
        rules(values) {
          const { newPassword } = values;
          return z
            .string({ required_error: '请再次输入新密码' })
            .min(1, { message: '请再次输入新密码' })
            .refine((value) => value === newPassword, {
              message: '两次输入的密码不一致',
            });
        },
        triggerFields: ['newPassword'],
      },
    },
  ];
});

async function handleSubmit(values: Record<string, any>) {
  try {
    const result = await changePasswordApi({
      newPassword: values.newPassword,
      oldPassword: values.oldPassword,
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
    message.success('密码修改成功，其他设备上的会话已失效');
  } catch (error) {
    message.error(
      error instanceof Error ? error.message : '密码修改失败，请稍后重试',
    );
  }
}
</script>
<template>
  <ProfilePasswordSetting
    class="w-1/3"
    :form-schema="formSchema"
    @submit="handleSubmit"
  />
</template>
