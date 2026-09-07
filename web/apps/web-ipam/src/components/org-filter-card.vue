<script setup lang="ts">
// 组织筛选卡：台账类页面共用的左侧组织树导航（选择组织过滤子网）。
import { computed } from 'vue';

import { Button, Card } from 'ant-design-vue';

import { Menu } from 'ant-design-vue';

import type { OrgTreeNode } from '#/api/ipam';

const props = defineProps<{
  orgTree: OrgTreeNode[];
  selectedOrgId: string;
}>();

const emit = defineEmits<{ (e: 'select', orgId: string): void }>();

const orgMenuItems = computed(() => {
  const walk = (nodes: OrgTreeNode[]): any[] =>
    nodes.map((n) => ({
      key: n.id,
      label: n.name,
      children: n.children?.length ? walk(n.children) : undefined,
    }));
  return walk(props.orgTree);
});

function onSelectMenu(key: string) {
  emit('select', key === props.selectedOrgId ? '' : key);
}
</script>

<template>
  <Card title="组织" class="w-36 shrink-0 self-start" :body-style="{ padding: '2px 0' }">
    <Menu
      class="org-menu max-h-[460px] overflow-auto"
      mode="vertical"
      :items="orgMenuItems"
      :selected-keys="selectedOrgId ? [selectedOrgId] : []"
      :inline-indent="10"
      @click="({ key }: any) => onSelectMenu(String(key))"
    />
    <div class="px-2 pb-1 pt-2">
      <Button size="small" block @click="onSelectMenu(selectedOrgId)">全部</Button>
    </div>
  </Card>
</template>

<style scoped>
.org-menu :deep(.ant-menu-item),
.org-menu :deep(.ant-menu-submenu-title) {
  height: 32px;
  line-height: 32px;
  font-size: 13px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.org-menu :deep(.ant-menu) {
  border-inline-end: none;
}
</style>
