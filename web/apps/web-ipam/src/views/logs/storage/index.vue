<script setup lang="ts">
import { IconifyIcon } from '@vben/icons';

import { Button } from 'ant-design-vue';

import LogStoragePanel from '#/components/log-storage-panel.vue';
import PageHeader from '#/components/page-header.vue';
import { $t } from '#/locales';
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:database"
      :title="$t('logs.storage.page.title')"
      :description="$t('logs.storage.page.description')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:hard-drive" class="size-3.5" />
          {{ $t('logs.storage.page.meta') }}
        </span>
        <RouterLink to="/logs-center/search">
          <Button size="small">
            <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />{{
              $t('logs.storage.page.queryOnline')
            }}
          </Button>
        </RouterLink>
      </template>
    </PageHeader>

    <LogStoragePanel kpi />
  </div>
</template>
