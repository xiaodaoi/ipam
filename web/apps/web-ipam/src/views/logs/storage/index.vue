<script setup lang="ts">
import { IconifyIcon } from '@vben/icons';

import { Button } from 'ant-design-vue';

import LogStoragePanel from '#/components/log-storage-panel.vue';
import PageHeader from '#/components/page-header.vue';
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="Log Center"
      icon="lucide:database"
      title="存储与归档"
      description="统一管理日志保留策略、ClickHouse 在线分区与按月 Parquet 归档，兼顾查询性能、存储成本与合规留存。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:hard-drive" class="size-3.5" />
          ClickHouse TTL + Parquet 归档
        </span>
        <RouterLink to="/logs-center/search">
          <Button size="small">
            <IconifyIcon icon="lucide:search" class="mr-1 size-3.5" />查询在线日志
          </Button>
        </RouterLink>
      </template>
    </PageHeader>

    <LogStoragePanel kpi />
  </div>
</template>
