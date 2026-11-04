<script setup lang="ts">
import type { BasicOption } from '@vben/types';

import type { VbenFormSchema } from '#/adapter/form';

import { computed, onMounted, ref } from 'vue';

import { ProfileBaseSetting } from '@vben/common-ui';

import { getUserInfoApi } from '#/api';
import { $t } from '#/locales';

const profileBaseSettingRef = ref();

const roleOptions = computed((): BasicOption[] => [
  { label: $t('core.profile.roles.admin'), value: 'super' },
  { label: $t('core.profile.roles.user'), value: 'user' },
  { label: $t('core.profile.roles.test'), value: 'test' },
]);

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      fieldName: 'realName',
      component: 'Input',
      label: $t('core.profile.fields.name'),
    },
    {
      fieldName: 'username',
      component: 'Input',
      label: $t('core.profile.fields.username'),
    },
    {
      fieldName: 'roles',
      component: 'Select',
      componentProps: {
        mode: 'tags',
        options: roleOptions.value,
      },
      label: $t('core.profile.fields.role'),
    },
    {
      fieldName: 'introduction',
      component: 'Textarea',
      label: $t('core.profile.fields.bio'),
    },
  ];
});

onMounted(async () => {
  const data = await getUserInfoApi();
  profileBaseSettingRef.value.getFormApi().setValues(data);
});
</script>
<template>
  <ProfileBaseSetting ref="profileBaseSettingRef" :form-schema="formSchema" />
</template>
