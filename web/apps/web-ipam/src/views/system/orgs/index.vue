<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import { Button, Card, Tree } from 'ant-design-vue';

import { createOrg, deleteOrg, listOrgTree, updateOrg, type OrgTreeNode } from '#/api/ipam';

const tree = ref<OrgTreeNode[]>([]);
const selected = ref<OrgTreeNode>();
const loading = ref(false);

function toAntTree(nodes: OrgTreeNode[]): any[] {
  return nodes.map((n) => ({
    key: n.id,
    title: n.name,
    children: n.children?.length ? toAntTree(n.children) : undefined,
  }));
}
const antTreeData = computed(() => toAntTree(tree.value));

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
  } finally {
    loading.value = false;
  }
}

const nameModal = ref({ initial: '', action: null as null | ((name: string) => Promise<void>) });
const [NameModal, nameModalApi] = useVbenModal({ draggable: true });

function askName(title: string, initial: string, action: (name: string) => Promise<void>) {
  nameModal.value = { initial, action };
  nameModalApi.setState({ title });
  nameModalApi.open();
}
async function confirmName() {
  const name = nameModal.value.initial.trim();
  if (!name) return;
  nameModalApi.close();
  await nameModal.value.action?.(name);
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
  <Card title="组织管理（全局主数据 ★）">
    <template #extra>
      <div class="flex gap-2">
        <Button size="small" type="primary" @click="addRoot">+ 根组织</Button>
        <Button size="small" :disabled="!selected" @click="addChild">+ 子组织</Button>
        <Button size="small" :disabled="!selected" @click="rename">改名</Button>
        <Button size="small" danger :disabled="!selected" @click="remove">删除</Button>
      </div>
    </template>

    <div class="mb-3 text-xs text-gray-400">
      多级自定义树（公司→部门→…）；被子网/资产引用或含子节点时删除会被 409 拒绝。
      此处是全部页面「组织下拉/树筛选」的单一数据源（§13.4 主数据原则）。
    </div>

    <Tree
      v-if="antTreeData.length"
      :tree-data="antTreeData"
      :selected-keys="selected ? [selected.id] : []"
      :default-expand-all="true"
      @select="(_, { node }) => {
        const find = (ns: OrgTreeNode[]): OrgTreeNode | undefined => {
          for (const n of ns) {
            if (n.id === node.key) return n;
            const hit = find(n.children ?? []);
            if (hit) return hit;
          }
        };
        selected = find(tree) ?? undefined;
      }"
    />
    <div v-else class="py-8 text-center text-gray-400">
      暂无组织——点击右上角「+ 根组织」创建第一个节点
    </div>
  </Card>
  <NameModal>
    <Input v-model:value="nameModal.initial" placeholder="组织名称" @pressEnter="confirmName" />
    <div class="mt-3 text-right">
      <Button @click="nameModalApi.close()">取消</Button>
      <Button type="primary" class="ml-1" @click="confirmName">确定</Button>
    </div>
  </NameModal>
</template>