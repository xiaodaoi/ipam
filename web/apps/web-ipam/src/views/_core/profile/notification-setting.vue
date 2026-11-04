<script setup lang="ts">
import { computed } from 'vue';

import { ProfileNotificationSetting } from '@vben/common-ui';

import { $t } from '#/locales';

const formSchema = computed(() => {
  return [
    {
      value: true,
      fieldName: 'accountPassword',
      label: $t('core.profile.notification.accountPassword'),
      description: $t('core.profile.notification.accountPasswordDesc'),
    },
    {
      value: true,
      fieldName: 'systemMessage',
      label: $t('core.profile.notification.systemMessage'),
      description: $t('core.profile.notification.systemMessageDesc'),
    },
    {
      value: true,
      fieldName: 'todoTask',
      label: $t('core.profile.notification.todo'),
      description: $t('core.profile.notification.todoDesc'),
    },
  ];
});
</script>
<template>
  <ProfileNotificationSetting :form-schema="formSchema" />
</template>
