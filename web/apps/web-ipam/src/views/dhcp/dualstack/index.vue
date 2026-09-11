<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import { Button, Card, Input, Select, Switch, Table, Tag, Tooltip, message } from 'ant-design-vue';

const showTotal = (t: number) => `共 ${t} 条`;

import {
  createDualstackTemplate,
  deleteDualstackTemplate,
  updateDualstackTemplate,
  listDualstackTemplates,
  createDualstackIdentity,
  deleteDualstackIdentity,
  listDualstackIdentities,
  listDualstackConflicts,
  type DualstackTemplate,
  type DualstackMatchScheme,
  type DualstackIdentity,
  type DualstackConflict,
} from '#/api/ipam';

const rows = ref<DualstackTemplate[]>([]);
const loading = ref(false);
const identityRows = ref<DualstackIdentity[]>([]);
const conflictRows = ref<DualstackConflict[]>([]);
const form = ref<{
  name: string; ipv4Cidr: string; ipv6Prefix: string; encoding: 'B' | 'A' | 'CUSTOM';
  expr: string; dnsSync: boolean; graceHours: number; matchScheme: DualstackMatchScheme;
}>({
  name: '',
  ipv4Cidr: '',
  ipv6Prefix: '',
  encoding: 'B',
  expr: '{v4.hextet4}',
  dnsSync: true,
  graceHours: 24,
  matchScheme: 'auto',
});
const editingId = ref('');

async function load() {
  loading.value = true;
  try {
    const d = await listDualstackTemplates();
    rows.value = d.items ?? [];
  } finally {
    loading.value = false;
  }
}
async function loadIdentities() {
  const d = await listDualstackIdentities();
  identityRows.value = d.items ?? [];
}
async function loadConflicts() {
  const d = await listDualstackConflicts();
  conflictRows.value = d.items ?? [];
}
function loadAll() {
  void load();
  void loadIdentities();
  void loadConflicts();
}
function edit(r: DualstackTemplate) {
  editingId.value = r.id;
  formModalApi.setState({ title: '编辑模板', confirmText: '保存修改' });
  formModalApi.open();
  form.value = {
    name: r.name, ipv4Cidr: r.ipv4Cidr, ipv6Prefix: r.ipv6Prefix, encoding: r.encoding,
    expr: r.expr, dnsSync: r.dnsSync ?? true, graceHours: r.graceHours ?? 24,
    matchScheme: r.matchScheme ?? 'auto',
  };
}
const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '双栈绑定模板', confirmText: '创建模板', onConfirm: () => add() });
function openAdd() {
  // 不能先 cancelEdit()（其 close() 异步，会在 open() 后把 isOpen 置回 false）
  editingId.value = '';
  form.value = { name: '', ipv4Cidr: '', ipv6Prefix: '', encoding: 'B', expr: '', dnsSync: true, graceHours: 24, matchScheme: 'auto' };
  formModalApi.setState({ title: '新建双栈绑定模板', confirmText: '创建模板' });
  formModalApi.open();
}
function cancelEdit() {
  editingId.value = '';
  formModalApi.close();
  form.value = {
    name: '', ipv4Cidr: '', ipv6Prefix: '',
    encoding: 'B', expr: '{v4.hextet4}', dnsSync: true, graceHours: 24, matchScheme: 'auto',
  };
}
async function add() {
  if (!form.value.name || !form.value.ipv4Cidr || !form.value.ipv6Prefix) return;
  if (editingId.value) {
    await updateDualstackTemplate(editingId.value, { ...form.value });
    message.success('模板已更新');
  } else {
    await createDualstackTemplate({ ...form.value });
    message.success('模板已创建');
  }
  cancelEdit();
  await load();
}
async function remove(id?: string) {
  if (id) await deleteDualstackTemplate(id);
  await load();
}

// ── MAC↔DUID 映射（M3-013）──
async function removeIdentity(mac: string) {
  await deleteDualstackIdentity(mac);
  message.success('映射已删除');
  await loadIdentities();
  await loadConflicts();
}

const mappingForm = ref<{ mac: string; duid: string; note: string }>({ mac: '', duid: '', note: '' });
const mappingMacOptions = ref<{ value: string; label: string }[]>([]);
function openMapping(c: DualstackConflict) {
  mappingMacOptions.value = (c.v4Macs ?? []).map((m) => ({ value: m, label: m }));
  mappingForm.value = { mac: (c.v4Macs ?? [])[0] ?? '', duid: c.duid, note: '' };
  mappingModalApi.open();
}
const [MappingModal, mappingModalApi] = useVbenModal({ draggable: true, title: '设为 MAC↔DUID 映射', confirmText: '创建映射', onConfirm: () => submitMapping() });
async function submitMapping() {
  if (!mappingForm.value.mac || !mappingForm.value.duid) return;
  await createDualstackIdentity({
    mac: mappingForm.value.mac,
    duid: mappingForm.value.duid,
    note: mappingForm.value.note || undefined,
  });
  message.success('映射已创建');
  mappingModalApi.close();
  await loadIdentities();
  await loadConflicts();
}
function canSetMapping(c: DualstackConflict): boolean {
  return (c.v4Macs?.length ?? 0) > 0;
}
function disableHint(c: DualstackConflict): string {
  if (c.reason === 'no_mac_signal') return '该冲突无 MAC 信号，无可选候选';
  if (c.reason === 'pinned_mismatch') return '钉死方案回落且无候选 MAC，需先在 v4 侧补充信号';
  return '无候选 MAC';
}

onMounted(loadAll);

const ENC_TEXT: Record<string, string> = { B: 'B 型', A: 'A 型', CUSTOM: '自定义' };
const EXAMPLE = '例：192.168.0.10 → 2407::192:168:0:10';

const SCHEME_TEXT: Record<DualstackMatchScheme, string> = {
  auto: '自动（优先级链）',
  option79: '中继 option79',
  'client-id': 'RFC4361',
  'duid-llt': 'DUID 内嵌 MAC',
  hostname: '主机名',
  admin: '人工映射',
};
const SCHEME_HINT: Record<DualstackMatchScheme, string> = {
  auto: '按 admin→option79→client-id→duid-llt→hostname 优先级链自动关联',
  option79: '需中继开 RFC6939 且 kea hook 已加载',
  'client-id': '需客户端 DHCPv4 携带 RFC4361 client-id（即 DUID）',
  'duid-llt': '从 DUID-LLT 内嵌 MAC 提取关联',
  hostname: '按主机名反查 v4 租约，同名歧义进冲突清单',
  admin: '仅认人工映射，未建映射进冲突清单',
};
const SCHEME_OPTIONS = (['auto', 'option79', 'client-id', 'duid-llt', 'hostname', 'admin'] as const).map(
  (v) => ({ value: v, label: SCHEME_TEXT[v] }),
);

const CONFLICT_TEXT: Record<string, string> = {
  ambiguous_hostname: '同名歧义',
  no_mac_signal: '无 MAC 信号',
  pinned_mismatch: '钉死方案回落',
};
const SOURCE_TEXT: Record<string, string> = { admin: '人工', auto: '自动' };
</script>

<template>
  <div class="p-4">
<Card title="双栈绑定模板（v4 池 ↔ v6 前缀映射）">
    <template #extra>
      <Button size="small" @click="loadAll()">刷新</Button>
    </template>

    <div class="mb-4">
      <Button type="primary" size="small" @click="openAdd()">+ 新建模板</Button>
    </div>
    <FormModal class="w-[860px]">
    <div class="flex flex-wrap items-end gap-2">
      <div>
        <div class="mb-1 text-xs text-gray-400">名称</div>
        <Input v-model:value="form.name" style="width: 150px" placeholder="办公-v4池A" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">IPv4 网段</div>
        <Input v-model:value="form.ipv4Cidr" style="width: 180px" placeholder="192.168.0.0/24" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">IPv6 前缀</div>
        <Input v-model:value="form.ipv6Prefix" style="width: 160px" placeholder="2407::/64" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">编码</div>
        <Select v-model:value="form.encoding" style="width: 100px"
          :options="(['B', 'A', 'CUSTOM'] as const).map((v) => ({ value: v, label: ENC_TEXT[v] }))" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">表达式</div>
        <Input v-model:value="form.expr" style="width: 160px" placeholder="{v4.hextet4}" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">匹配方案</div>
        <Select v-model:value="form.matchScheme" style="width: 150px" :options="SCHEME_OPTIONS" />
      </div>
      <div class="flex items-center gap-1 pb-[2px]">
        <span class="text-xs">DNS 同步</span>
        <Switch v-model:checked="form.dnsSync" size="small" />
      </div>
      <div class="w-full text-xs text-gray-400">{{ EXAMPLE }}——daemon 按租约 IPv4 最长前缀自动选模板</div>
      <div class="w-full text-xs text-gray-400">匹配方案：{{ SCHEME_HINT[form.matchScheme] }}</div>
    </div>
    </FormModal>

        <Table
          :data-source="rows"
          :columns="[
            { title: '名称', dataIndex: 'name' },
            { title: 'IPv4 网段', dataIndex: 'ipv4Cidr' },
            { title: 'IPv6 前缀', dataIndex: 'ipv6Prefix' },
            { title: '编码', dataIndex: 'encoding', width: 80 },
            { title: '表达式', dataIndex: 'expr' },
            { title: '匹配方案', dataIndex: 'matchScheme', width: 130 },
            { title: 'DNS 同步', dataIndex: 'dnsSync', width: 90 },
            { title: '宽限(h)', dataIndex: 'graceHours', width: 80 },
            { title: '操作', dataIndex: 'op', width: 150 },
          ]"
          row-key="id"
          size="small"
          :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'encoding'">
              <Tag>{{ ENC_TEXT[record.encoding] ?? record.encoding }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'matchScheme'">
              <Tag>{{ SCHEME_TEXT[record.matchScheme] ?? record.matchScheme ?? 'auto' }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'dnsSync'">
              <Tag :color="record.dnsSync ? 'green' : 'default'">{{ record.dnsSync ? '开' : '关' }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'op'">
              <div class="flex items-center gap-1">
                <Button size="small" @click="edit(record as DualstackTemplate)">编辑</Button>
                <Button size="small" danger @click="remove(record.id)">删除</Button>
              </div>
            </template>
          </template>
        </Table>
  </Card>

  <Card title="MAC↔DUID 映射（人工 + 自动学习）" class="mt-4">
    <template #extra>
      <Button size="small" @click="loadIdentities()">刷新</Button>
    </template>
    <Table
      :data-source="identityRows"
      :columns="[
        { title: 'MAC', dataIndex: 'mac' },
        { title: 'DUID', dataIndex: 'duid' },
        { title: '来源', dataIndex: 'source', width: 90 },
        { title: '备注', dataIndex: 'note' },
        { title: '操作', dataIndex: 'op', width: 90 },
      ]"
      row-key="mac"
      size="small"
      :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.dataIndex === 'source'">
          <Tag :color="record.source === 'admin' ? 'blue' : 'default'">{{ SOURCE_TEXT[record.source] ?? record.source }}</Tag>
        </template>
        <template v-else-if="column.dataIndex === 'note'">
          {{ record.note || '-' }}
        </template>
        <template v-else-if="column.dataIndex === 'op'">
          <Button size="small" danger @click="removeIdentity(record.mac)">删除</Button>
        </template>
      </template>
    </Table>
  </Card>

  <Card title="未解析冲突" class="mt-4">
    <template #extra>
      <Button size="small" @click="loadConflicts()">刷新</Button>
    </template>
    <Table
      :data-source="conflictRows"
      :columns="[
        { title: '主机名', dataIndex: 'hostname' },
        { title: 'v6 地址', dataIndex: 'v6Ip' },
        { title: 'v4 MAC 候选', dataIndex: 'v4Macs' },
        { title: '原因', dataIndex: 'reason', width: 120 },
        { title: '操作', dataIndex: 'op', width: 120 },
      ]"
      row-key="duid"
      size="small"
      :pagination="{ pageSize: 20, showSizeChanger: true, showTotal }"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.dataIndex === 'hostname'">
          {{ record.hostname || '-' }}
        </template>
        <template v-else-if="column.dataIndex === 'v6Ip'">
          {{ record.v6Ip || '-' }}
        </template>
        <template v-else-if="column.dataIndex === 'v4Macs'">
          <template v-if="record.v4Macs?.length">
            <div v-for="m in record.v4Macs" :key="m">{{ m }}</div>
          </template>
          <span v-else>-</span>
        </template>
        <template v-else-if="column.dataIndex === 'reason'">
          <Tag :color="record.reason === 'ambiguous_hostname' ? 'orange' : record.reason === 'pinned_mismatch' ? 'red' : 'default'">
            {{ CONFLICT_TEXT[record.reason] ?? record.reason }}
          </Tag>
        </template>
        <template v-else-if="column.dataIndex === 'op'">
          <Button v-if="canSetMapping(record as DualstackConflict)" size="small" @click="openMapping(record as DualstackConflict)">设为映射</Button>
          <Tooltip v-else :title="disableHint(record as DualstackConflict)">
            <span><Button size="small" disabled>设为映射</Button></span>
          </Tooltip>
        </template>
      </template>
    </Table>
  </Card>

  <MappingModal class="w-[420px]">
    <div class="flex flex-col gap-3">
      <div>
        <div class="mb-1 text-xs text-gray-400">v4 MAC 选择</div>
        <Select v-if="mappingMacOptions.length > 1" v-model:value="mappingForm.mac" style="width: 100%" :options="mappingMacOptions" />
        <Input v-else v-model:value="mappingForm.mac" readonly />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">DUID</div>
        <Input v-model:value="mappingForm.duid" readonly />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">备注</div>
        <Input v-model:value="mappingForm.note" placeholder="可选" />
      </div>
    </div>
  </MappingModal>
  </div>
</template>
