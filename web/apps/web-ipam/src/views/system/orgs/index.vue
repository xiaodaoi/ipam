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
      :title="$t('system.orgs.title')"
      :description="$t('system.orgs.desc')"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:info" class="size-3.5" />
          {{ $t('system.orgs.lockedHint') }}
        </span>
      </template>
    </PageHeader>

    <OrgManageCard />
  </div>
</template>
