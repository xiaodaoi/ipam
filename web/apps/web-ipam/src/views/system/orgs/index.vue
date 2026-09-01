<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, Tag, Tree, message } from 'ant-design-vue';

import { createOrg, deleteOrg, listOrgTree, updateOrg, type OrgTreeNode } from '#/api/ipam';

const tree = ref<OrgTreeNode[]>([]);
const selected = ref<OrgTreeNode>();
const loading = ref(false);
const expandedKeys = ref<string[]>([]);

interface AntNode {
  key: string;
  title: string;
  isLeaf?: boolean;
  children?: AntNode[];
  org?: OrgTreeNode;
}

function toAntTree(nodes: OrgTreeNode[]): AntNode[] {
  return nodes.map((n) => ({
    key: n.id,
    title: n.name,
    org: n,
    isLeaf: !(n.children?.length),
    children: n.children?.length ? toAntTree(n.children) : undefined,
  }));
}
const antTreeData = computed(() => toAntTree(tree.value));

/** 全部节点 key（用于展开全部） */
function collectKeys(nodes: OrgTreeNode[]): string[] {
  const out: string[] = [];
  const walk = (ns: OrgTreeNode[]) => {
    for (const n of ns) {
      out.push(n.id);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

/** 组织总数（含子孙） */
const totalCount = computed(() => collectKeys(tree.value).length);

function expandAll() {
  expandedKeys.value = collectKeys(tree.value);
}
function collapseAll() {
  expandedKeys.value = [];
}

async function load() {
  loading.value = true;
  try {
    tree.value = await listOrgTree();
    if (selected.value) {
      // 刷新后按 id 找回选中（结构可能已变）
      const find = (ns: OrgTreeNode[]): OrgTreeNode | undefined => {
        for (const n of ns) {
          if (n.id === selected.value?.id) return n;
          const hit = find(n.children ?? []);
          if (hit) return hit;
        }
      };
      selected.value = find(tree.value);
    }
    expandedKeys.value = collectKeys(tree.value);
  } finally {
    loading.value = false;
  }
}

function onSelect(_keys: unknown[], info: unknown) {
  const node = (info as { node?: { org?: OrgTreeNode } }).node;
  if (node?.org) selected.value = node.org;
}

const nameInput = ref('');
const nameAction = ref<((name: string) => Promise<void>) | null>(null);
const [NameModal, nameModalApi] = useVbenModal({ draggable: true, confirmText: '确定', onConfirm: () => confirmName() });

function askName(title: string, initial: string, action: (name: string) => Promise<void>) {
  nameInput.value = initial;
  nameAction.value = action;
  nameModalApi.setState({ title });
  nameModalApi.open();
}
async function confirmName() {
  const name = nameInput.value.trim();
  if (!name) {
    message.warning('请输入组织名称');
    return;
  }
  nameModalApi.close();
  const action = nameAction.value;
  if (!action) return;
  try {
    await action(name);
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
}

function addRoot() {
  askName('根组织名称', '', async (name) => {
    await createOrg({ parentId: null, name });
    await load();
  });
}
function addChild() {
  const sel = selected.value;
  if (!sel) return;
  askName(`在「${sel.name}」下新建子组织`, '', async (name) => {
    await createOrg({ parentId: sel.id, name });
    await load();
  });
}
function rename() {
  const sel = selected.value;
  if (!sel) return;
  askName('改名', sel.name, async (name) => {
    if (name === sel.name) return;
    await updateOrg(sel.id, { name });
    await load();
  });
}
async function remove() {
  if (!selected.value) return;
  if (!window.confirm(`删除「${selected.value.name}」？存在子节点/子网/资产引用时将被 409 拒绝。`)) return;
  try {
    await deleteOrg(selected.value.id);
    selected.value = undefined;
    await load();
  } catch (e) {
    // ORG_IN_USE 等错误已由请求层 message 弹出（detail 透传）
    await load();
  }
}
onMounted(load);
</script>

<template>
  <div>
  <Card title="组织管理（全局主数据 ★）">
    <template #extra>
      <div class="flex gap-2">
        <Button size="small" type="primary" @click="addRoot">+ 根组织</Button>
        <Button size="small" :disabled="!selected" @click="addChild">+ 子组织</Button>
        <Button size="small" :disabled="!selected" @click="rename">改名</Button>
        <Button size="small" danger :disabled="!selected" @click="remove">删除</Button>
      </div>
    </template>

    <div class="mb-3 flex items-center justify-between gap-2">
      <div class="text-xs text-gray-400">
        多级自定义树（公司→部门→…）；被子网/资产引用或含子节点时删除会被 409 拒绝。
        此处是全部页面「组织下拉/树筛选」的单一数据源（§13.4 主数据原则）。
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <Tag color="blue" class="m-0">{{ totalCount }} 个组织</Tag>
        <Button size="small" @click="expandAll">展开全部</Button>
        <Button size="small" @click="collapseAll">收起全部</Button>
      </div>
    </div>

    <Tree
      v-if="antTreeData.length"
      class="org-tree"
      :tree-data="antTreeData"
      :selected-keys="selected ? [selected.id] : []"
      :expanded-keys="expandedKeys"
      show-line
      block-node
      :loading="loading"
      @select="onSelect"
      @expand="(keys: (string | number)[]) => (expandedKeys = keys as string[])"
    >
      <template #titleRender="{ node }">
        <div class="flex items-center gap-1.5 py-0.5">
          <IconifyIcon
            :icon="node.isLeaf ? 'lucide:corner-down-right' : 'lucide:building-2'"
            class="shrink-0"
            :class="node.isLeaf ? 'text-gray-400' : 'text-primary'"
          />
          <span class="truncate">{{ node.title }}</span>
          <Tag v-if="!node.isLeaf" color="processing" class="m-0 scale-90">
            {{ (node.children ?? []).length }}
          </Tag>
        </div>
      </template>
    </Tree>
    <div v-else class="py-8 text-center text-gray-400">
      暂无组织——点击右上角「+ 根组织」创建第一个节点
    </div>
  </Card>
  <NameModal>
    <div class="flex items-center gap-2">
      <span class="shrink-0 text-sm font-medium text-gray-600">组织名称</span>
      <Input
        v-model:value="nameInput"
        placeholder="请输入组织名称"
        autofocus
        class="flex-1"
        @pressEnter="confirmName"
      />
    </div>
  </NameModal>
  </div>
</template>

<style scoped>
.org-tree :deep(.ant-tree-node-content-wrapper) {
  border-radius: 6px;
  padding: 2px 6px;
  transition: background-color 0.15s ease;
}
.org-tree :deep(.ant-tree-node-content-wrapper:hover) {
  background: rgba(99, 102, 241, 0.08);
}
.org-tree :deep(.ant-tree-node-selected) {
  background: rgba(99, 102, 241, 0.14) !important;
}
.org-tree :deep(.ant-tree-treenode) {
  padding: 1px 0;
}
</style>