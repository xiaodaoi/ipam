<script setup lang="ts">
import { IconifyIcon } from '@vben/icons';

import OrgManageCard from '#/components/org-manage-card.vue';
import PageHeader from '#/components/page-header.vue';

defineOptions({ name: 'SystemOrgs' });
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:git-branch"
      title="组织管理"
      description="全站唯一组织主数据：公司 → 部门 → … 多级树，DHCP/DNS/资产/日志均引用同一棵树。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:info" class="size-3.5" />
          被引用或含子节点时不可删除
        </span>
      </template>
    </PageHeader>

    <OrgManageCard />
  </div>
</template>
