<script setup lang="ts">
import type { VbenFormSchema } from '#/adapter/form';

import { computed } from 'vue';

import { ProfilePasswordSetting, z } from '@vben/common-ui';
import { useAccessStore, useUserStore } from '@vben/stores';

import { message } from 'ant-design-vue';

import { changePasswordApi } from '#/api';
import { $t } from '#/locales';

const accessStore = useAccessStore();
const userStore = useUserStore();

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      fieldName: 'oldPassword',
      label: $t('core.profile.password.oldLabel'),
      component: 'VbenInputPassword',
      componentProps: {
        placeholder: $t('core.profile.password.oldPlaceholder'),
      },
      rules: z
        .string()
        .min(1, { message: $t('core.profile.password.oldPlaceholder') }),
    },
    {
      fieldName: 'newPassword',
      label: $t('core.profile.password.newLabel'),
      component: 'VbenInputPassword',
      componentProps: {
        passwordStrength: true,
        placeholder: $t('core.profile.password.newPlaceholder'),
      },
      rules: z
        .string()
        .min(10, { message: $t('core.profile.password.min') })
        .max(128, { message: $t('core.profile.password.max') }),
    },
    {
      fieldName: 'confirmPassword',
      label: $t('core.profile.password.confirmLabel'),
      component: 'VbenInputPassword',
      componentProps: {
        passwordStrength: true,
        placeholder: $t('core.profile.password.confirmPlaceholder'),
      },
      dependencies: {
        rules(values) {
          const { newPassword } = values;
          return z
            .string({ required_error: $t('core.profile.password.required') })
            .min(1, { message: $t('core.profile.password.required') })
            .refine((value) => value === newPassword, {
              message: $t('core.profile.password.mismatch'),
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
    message.success($t('core.profile.password.success'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.password.failed'),
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
