<script setup lang="ts">
import type { OrgTreeNode } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, message, Popconfirm, Tag, Tree } from 'ant-design-vue';

import { createOrg, deleteOrg, listOrgTree, reorderOrgs, updateOrg } from '#/api/ipam';
import { $t } from '#/locales';

const tree = ref<OrgTreeNode[]>([]);
const selected = ref<OrgTreeNode>();
const loading = ref(false);
const expandedKeys = ref<string[]>([]);
const dragId = ref<string>();

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
    isLeaf: !n.children?.length,
    children: n.children?.length ? toAntTree(n.children) : undefined,
  }));
}
const antTreeData = computed(() => toAntTree(tree.value));

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

const totalCount = computed(() => collectKeys(tree.value).length);

function expandAll() {
  expandedKeys.value = collectKeys(tree.value);
}
function collapseAll() {
  expandedKeys.value = [];
}

async function load(keepSelection = true) {
  loading.value = true;
  try {
    tree.value = await listOrgTree();
    if (keepSelection && selected.value) {
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

// ── 同级拖拽排序 ──
function findChildrenOf(nodes: OrgTreeNode[], parentId: null | string): null | OrgTreeNode[] {
  if (parentId === null || parentId === undefined || parentId === '') {
    return nodes;
  }
  const walk = (ns: OrgTreeNode[]): null | OrgTreeNode[] => {
    for (const n of ns) {
      if (n.id === parentId) return n.children ?? [];
      const hit = walk(n.children ?? []);
      if (hit) return hit;
    }
    return null;
  };
  return walk(nodes);
}

function allowDrop(info: any) {
  return info.dropPosition !== 0; // 仅允许同级排序，禁止拖入节点变更父级
}

async function onDrop(info: any) {
  const dragOrg = (info.dragNode as unknown as { org?: OrgTreeNode }).org;
  const dropOrg = (info.node as unknown as { org?: OrgTreeNode }).org;
  const dropToGap: boolean = info.dropToGap;
  const dropPosition: number = info.dropPosition;
  dragId.value = undefined;
  if (!dragOrg || !dropOrg) {
    await load();
    return;
  }
  if (!dropToGap || dropPosition === 0) {
    message.warning($t('shared.orgManage.warnSameLevelDrag'));
    await load();
    return;
  }
  const parentId = dragOrg.parentId ? String(dragOrg.parentId) : null;
  const dropParent = dropOrg.parentId ? String(dropOrg.parentId) : null;
  if (dropParent !== parentId) {
    message.warning($t('shared.orgManage.warnSameLevelOnly'));
    await load();
    return;
  }
  const siblings = findChildrenOf(tree.value, parentId);
  if (!siblings || siblings.length < 2) {
    await load();
    return;
  }
  // 在当前显示顺序内移动 drag 到 drop 前/后
  const arr = siblings.map((n) => n.id);
  const from = arr.indexOf(dragOrg.id);
  const to = arr.indexOf(dropOrg.id);
  if (from === -1 || to === -1 || from === to) {
    await load();
    return;
  }
  const moved = arr[from];
  if (!moved) {
    await load();
    return;
  }
  arr.splice(from, 1);
  let insertAt = arr.indexOf(dropOrg.id);
  if (insertAt < 0) {
    await load();
    return;
  }
  if (dropPosition === 1) insertAt += 1; // 拖到目标之后
  arr.splice(insertAt, 0, moved);
  try {
    await reorderOrgs(parentId, arr);
    message.success($t('shared.orgManage.reorderOk'));
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('shared.orgManage.reorderFail'));
  }
  await load();
}

const nameInput = ref('');
const nameAction = ref<((name: string) => Promise<void>) | null>(null);
const [NameModal, nameModalApi] = useVbenModal({ draggable: true, confirmText: $t('shared.orgManage.confirmText'), onConfirm: () => confirmName() });

function askName(title: string, initial: string, action: (name: string) => Promise<void>) {
  nameInput.value = initial;
  nameAction.value = action;
  nameModalApi.setState({ title });
  nameModalApi.open();
}
async function confirmName() {
  const name = nameInput.value.trim();
  if (!name) {
    message.warning($t('shared.orgManage.nameRequired'));
    return;
  }
  if ([...name].length > 64) {
    message.warning($t('shared.orgManage.nameTooLong'));
    return;
  }
  nameModalApi.close();
  const action = nameAction.value;
  if (!action) return;
  try {
    await action(name);
  } catch (error) {
    message.error(error instanceof Error ? error.message : $t('shared.orgManage.actionFail'));
  }
}

function addRoot() {
  askName($t('shared.orgManage.rootName'), '', async (name) => {
    await createOrg({ parentId: null, name });
    await load(false);
  });
}
function addChild() {
  const sel = selected.value;
  if (!sel) return;
  askName($t('shared.orgManage.childName', { name: sel.name }), '', async (name) => {
    await createOrg({ parentId: sel.id, name });
    await load();
  });
}
function rename() {
  const sel = selected.value;
  if (!sel) return;
  askName($t('shared.orgManage.rename'), sel.name, async (name) => {
    if (name === sel.name) return;
    await updateOrg(sel.id, { name });
    await load();
  });
}
async function remove() {
  if (!selected.value) return;
  try {
    await deleteOrg(selected.value.id);
    selected.value = undefined;
    await load(false);
  } catch {
    await load();
  }
}
onMounted(() => load(false));
</script>

<template>
  <Card :title="$t('shared.orgManage.title')">
      <template #extra>
        <div class="flex flex-wrap items-center justify-end gap-2">
          <Button type="primary" size="small" @click="addRoot">
            <IconifyIcon icon="lucide:folder-plus" class="mr-1" />{{ $t('shared.orgManage.addRoot') }}
          </Button>
          <Button size="small" :disabled="!selected" @click="addChild">
            <IconifyIcon icon="lucide:folder-tree" class="mr-1" />{{ $t('shared.orgManage.addChild') }}
          </Button>
          <Button size="small" :disabled="!selected" @click="rename">
            <IconifyIcon icon="lucide:pencil" class="mr-1" />{{ $t('shared.orgManage.renameBtn') }}
          </Button>
          <Popconfirm
            :title="$t('shared.orgManage.deleteTitle')"
            :description="$t('shared.orgManage.deleteDesc')"
            :ok-text="$t('shared.orgManage.delete')"
            :cancel-text="$t('shared.orgManage.cancel')"
            @confirm="remove"
          >
            <Button size="small" danger :disabled="!selected">
              <IconifyIcon icon="lucide:trash-2" class="mr-1" />{{ $t('shared.orgManage.deleteBtn') }}
            </Button>
          </Popconfirm>
        </div>
      </template>

      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div class="ipam-hint">
          {{ $t('shared.orgManage.treeHint') }}
          <span class="mx-1 font-medium text-primary">
            {{ $t('shared.orgManage.treeHintDrag') }}
          </span>
          {{ $t('shared.orgManage.treeHintTail') }}
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <Tag color="blue" class="m-0">
            {{ $t('shared.orgManage.orgCount', { n: totalCount }) }}
          </Tag>
          <Button size="small" @click="expandAll">
            {{ $t('shared.orgManage.expandAll') }}
          </Button>
          <Button size="small" @click="collapseAll">
            {{ $t('shared.orgManage.collapseAll') }}
          </Button>
        </div>
      </div>

      <Tree
        v-if="antTreeData.length > 0"
        class="org-tree"
        :tree-data="antTreeData"
        :selected-keys="selected ? [selected.id] : []"
        :expanded-keys="expandedKeys"
        :draggable="true"
        :allow-drop="allowDrop"
        block-node
        show-line
        :loading="loading"
        @select="onSelect"
        @dragstart="(n: any) => (dragId = n.node?.org?.id)"
        @drop="onDrop"
        @expand="(keys: (string | number)[]) => (expandedKeys = keys as string[])"
      >
        <template #title="node">
          <div
            class="flex items-center gap-1.5 py-0.5"
            :class="dragId === node.org?.id ? 'opacity-60' : ''"
          >
            <IconifyIcon
              :icon="node.isLeaf ? 'lucide:corner-down-right' : 'lucide:building-2'"
              class="shrink-0"
              :class="node.isLeaf ? 'text-muted-foreground' : 'text-primary'"
            />
            <span class="truncate">{{ node.title }}</span>
            <Tag v-if="!node.isLeaf" color="processing" class="m-0 scale-90">
              {{ (node.children ?? []).length }}
            </Tag>
          </div>
        </template>
      </Tree>
      <div v-else class="py-8 text-center text-muted-foreground">
        {{ $t('shared.orgManage.empty') }}
      </div>
    </Card>
    <NameModal>
      <div class="flex items-center gap-2">
        <span class="shrink-0 text-sm font-medium text-muted-foreground">
          {{ $t('shared.orgManage.nameLabel') }}
        </span>
        <Input
          v-model:value="nameInput"
          :maxlength="64"
          :placeholder="$t('shared.orgManage.namePlaceholder')"
          autofocus
          class="flex-1"
          @press-enter="confirmName"
        />
      </div>
    </NameModal>
</template>

<style scoped>
.org-tree :deep(.ant-tree-treenode) {
  padding: 2px 0;
}
.org-tree :deep(.ant-tree-node-content-wrapper) {
  border-radius: 6px;
  transition: background-color 0.15s ease;
}
.org-tree :deep(.ant-tree-node-content-wrapper:hover) {
  background: var(--color-accent);
}
.org-tree :deep(.ant-tree-node-selected) {
  background: var(--color-accent) !important;
}
.org-tree :deep(.ant-tree-node-content-wrapper:hover .drag-hint) {
  opacity: 1;
}
</style>
