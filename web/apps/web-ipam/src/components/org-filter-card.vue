<script setup lang="ts">
import type { OrgTreeNode } from '#/api/ipam';

// 组织筛选卡：台账类页面共用的左侧组织树导航（选择组织过滤子网/资产）。
// 与子网管理页的组织面板保持一致的交互：搜索、全部组织、点选下钻、再点取消。
import { computed, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, Tree } from 'ant-design-vue';

const props = defineProps<{
  orgTree: OrgTreeNode[];
  selectedOrgId: string;
}>();

const emit = defineEmits<{ (e: 'select', orgId: string): void }>();

interface OrgAntNode {
  children?: OrgAntNode[];
  isLeaf?: boolean;
  key: string;
  title: string;
}

const keyword = ref('');
const expandedKeys = ref<string[]>([]);

function collectKeys(nodes: OrgTreeNode[]): string[] {
  const out: string[] = [];
  const walk = (list: OrgTreeNode[]) => {
    for (const n of list) {
      out.push(n.id);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

function toAntNodes(nodes: OrgTreeNode[], kw: string): OrgAntNode[] {
  const out: OrgAntNode[] = [];
  for (const n of nodes) {
    const children = toAntNodes(n.children ?? [], kw);
    const hit = !kw || n.name.toLowerCase().includes(kw);
    if (!hit && children.length === 0) continue;
    out.push({
      key: n.id,
      title: n.name,
      isLeaf: !n.children?.length,
      children: children.length > 0 ? children : undefined,
    });
  }
  return out;
}

const treeData = computed(() =>
  toAntNodes(props.orgTree, keyword.value.trim().toLowerCase()),
);

const effectiveExpandedKeys = computed(() =>
  keyword.value.trim() ? collectAntKeys(treeData.value) : expandedKeys.value,
);

function collectAntKeys(nodes: OrgAntNode[]): string[] {
  const out: string[] = [];
  const walk = (list: OrgAntNode[]) => {
    for (const n of list) {
      out.push(n.key);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

const selectedName = computed(() => {
  let name = '';
  const walk = (nodes: OrgTreeNode[]): boolean => {
    for (const n of nodes) {
      if (n.id === props.selectedOrgId) {
        name = n.name;
        return true;
      }
      if (n.children?.length && walk(n.children)) return true;
    }
    return false;
  };
  walk(props.orgTree);
  return name;
});

function onSelectOrg(key: string) {
  emit('select', key === props.selectedOrgId ? '' : key);
}

function onTreeSelect(_keys: unknown, info: unknown) {
  const key = (info as { node?: { key?: string } }).node?.key;
  if (key) onSelectOrg(String(key));
}

function onTreeExpand(keys: unknown) {
  expandedKeys.value = Array.isArray(keys) ? keys.map(String) : [];
}

watch(
  () => props.orgTree,
  (list) => {
    expandedKeys.value = collectKeys(list);
  },
  { immediate: true },
);
</script>

<template>
  <Card class="ipam-side-card">
    <template #title>
      <span class="ipam-card-title">
        <IconifyIcon icon="lucide:building-2" />
        组织架构
      </span>
    </template>

    <Input
      v-model:value="keyword"
      allow-clear
      placeholder="搜索组织名称"
      size="small"
    >
      <template #prefix>
        <IconifyIcon icon="lucide:search" class="size-3.5 text-gray-400" />
      </template>
    </Input>

    <button
      class="ipam-org-all"
      type="button"
      :data-active="!selectedOrgId"
      @click="emit('select', '')"
    >
      <span class="ipam-org-all__label">
        <IconifyIcon icon="lucide:layers" class="size-4" />
        全部组织
      </span>
      <span class="ipam-org-badge">{{ orgTree.length }}</span>
    </button>

    <Tree
      v-if="treeData.length > 0"
      class="ipam-org-tree"
      :tree-data="treeData"
      :expanded-keys="effectiveExpandedKeys"
      :selected-keys="selectedOrgId ? [selectedOrgId] : []"
      block-node
      @expand="onTreeExpand"
      @select="onTreeSelect"
    />

    <div v-else class="ipam-empty ipam-empty--compact">
      <div class="ipam-empty__title">
        {{ keyword ? '没有匹配的组织' : '尚未创建组织' }}
      </div>
      <div class="ipam-empty__desc">
        {{ keyword ? '换个关键词试试' : '请先在子网管理 → 组织管理中创建' }}
      </div>
    </div>

    <div class="ipam-org-panel__foot">
      <IconifyIcon icon="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
      <span>
        {{
          selectedOrgId
            ? `当前范围：${selectedName || '已选组织'}（含下级组织）`
            : '点选组织过滤台账，再点取消'
        }}
      </span>
    </div>

    <Button size="small" block class="mt-1" @click="emit('select', '')">
      重置筛选
    </Button>
  </Card>
</template>
