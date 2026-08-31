<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { useVbenModal } from '@vben/common-ui';

import {
  Button,
  Card,
  Input,
  InputNumber,
  message,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  createDhcpClass,
  createDhcpOption,
  deleteDhcpClass,
  deleteDhcpOption,
  listDhcpClasses,
  listDhcpOptions,
  updateDhcpClass,
  updateDhcpOption,
  type DhcpClassOptionIn,
  type DhcpClassRow,
  type DhcpOptionRow,
} from '#/api/ipam';

const optRows = ref<DhcpOptionRow[]>([]);
const clsRows = ref<DhcpClassRow[]>([]);
const loading = ref(false);
const optForm = ref({ code: 3, name: 'routers', data: '' });
const clsForm = ref({ name: '', test: "option[61].hex == option[61].hex", rows: [] as DhcpClassOptionIn[] });
const editingOpt = ref<string>();
const editingCls = ref<string>();
const [OptModal, optModalApi] = useVbenModal({ draggable: true, title: '标准选项' });
const [ClsModal, clsModalApi] = useVbenModal({ draggable: true, title: '类匹配规则' });
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
  loading.value = true;
  try {
    const [o, c] = await Promise.all([listDhcpOptions(), listDhcpClasses()]);
    optRows.value = o.items ?? [];
    clsRows.value = c.items ?? [];
  } finally {
    loading.value = false;
  }
}

async function addOption() {
  if (!optForm.value.name || !optForm.value.data) {
    message.warning('选项名与值必填');
    return;
  }
  try {
    if (editingOpt.value) {
      await updateDhcpOption(editingOpt.value, {
        optionCode: optForm.value.code, name: optForm.value.name, data: optForm.value.data,
      });
      message.success('选项已更新');
    } else {
      await createDhcpOption({
        optionCode: optForm.value.code, name: optForm.value.name, data: optForm.value.data,
      });
      message.success('选项已创建');
    }
    editingOpt.value = undefined;
    optModalApi.close();
    optForm.value = { code: optForm.value.code, name: 'routers', data: '' };
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  await load();
}
async function toggleOption(r: DhcpOptionRow, enabled: boolean) {
  try {
    await updateDhcpOption(r.id, { enabled });
    message.success(enabled ? '已启用' : '已停用');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  await load();
}
function editOpt(r: DhcpOptionRow) {
  editingOpt.value = r.id;
  optModalApi.setState({ title: '编辑选项' });
  optModalApi.open();
  optForm.value = { code: r.optionCode, name: r.name, data: r.data };
}
async function removeOption(id?: string) {
  if (!id) return;
  try {
    await deleteDhcpOption(id);
    message.success('已删除');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '删除失败');
  }
  await load();
}

function addClassRow() {
  clsForm.value.rows.push({ optionCode: 3, name: 'routers', data: '' });
}
function removeClassRow(i: number) {
  clsForm.value.rows.splice(i, 1);
}
async function addClass() {
  if (!clsForm.value.name) {
    message.warning('类名必填');
    return;
  }
  if (clsForm.value.rows.some((r) => !r.name || !r.data)) {
    message.warning('类内选项的名称与值必填');
    return;
  }
  try {
    if (editingCls.value) {
      await updateDhcpClass(editingCls.value, { test: clsForm.value.test, options: clsForm.value.rows });
      message.success('类已更新');
    } else {
      await createDhcpClass({
        name: clsForm.value.name,
        options: clsForm.value.rows,
        test: clsForm.value.test,
      });
      message.success('类已创建');
    }
    editingCls.value = undefined;
    clsModalApi.close();
    clsForm.value = { name: '', test: clsForm.value.test, rows: [] };
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  await load();
}
async function toggleClass(r: DhcpClassRow, enabled: boolean) {
  try {
    await updateDhcpClass(r.id, { enabled });
    message.success(enabled ? '已启用' : '已停用');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '操作失败');
  }
  await load();
}
function editCls(r: DhcpClassRow) {
  editingCls.value = r.id;
  clsModalApi.setState({ title: '编辑类' });
  clsModalApi.open();
  clsForm.value = { name: r.name, test: r.test, rows: [...(r.options ?? [])] };
}
async function removeClass(id?: string) {
  if (!id) return;
  try {
    await deleteDhcpClass(id);
    message.success('已删除');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '删除失败');
  }
  await load();
}
function cancelEditOpt() {
  editingOpt.value = undefined;
  optModalApi.close();
  optForm.value = { code: optForm.value.code, name: 'routers', data: '' };
}
function cancelEditCls() {
  editingCls.value = undefined;
  clsModalApi.close();
  clsForm.value = { name: '', test: clsForm.value.test, rows: [] };
}
const optsSummary = (options?: DhcpClassOptionIn[]) =>
  (options ?? []).map((o) => `${o.name}=${o.data}`).join(', ') || '-';
onMounted(() => {
  void load();
  timer = setInterval(load, 15_000);
});
onBeforeUnmount(() => timer && clearInterval(timer));
</script>

<template>
  <div class="grid grid-cols-1 gap-4">
    <Card title="标准选项（C-02，全局 option-data）">
      <div class="mb-3">
        <Button type="primary" size="small" @click="optModalApi.open()">+ 添加选项</Button>
      </div>
      <OptModal class="w-[720px]">
      <div class="flex flex-wrap items-end gap-2">
        <div>
          <div class="mb-1 text-xs text-gray-400">选项码</div>
          <InputNumber v-model:value="optForm.code" :min="1" :max="255" style="width: 90px" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">Kea 选项名</div>
          <Input v-model:value="optForm.name" style="width: 180px" placeholder="routers" />
        </div>
        <div>
          <div class="mb-1 text-xs text-gray-400">值</div>
          <Input v-model:value="optForm.data" style="width: 180px" placeholder="192.168.9.1" />
        </div>
        <Button type="primary" @click="addOption">{{ editingOpt ? '保存修改' : '创建选项' }}</Button>
        <Button v-if="editingOpt" @click="cancelEditOpt">取消编辑</Button>
        <div class="w-full text-xs text-gray-400">
          变更后经 Kea config-set 原子下发；disabled 选项不进配置。
        </div>
      </div>
      </OptModal>
      <Table
        :data-source="optRows"
        :columns="[
          { title: '码', dataIndex: 'optionCode', width: 70 },
          { title: '选项名', dataIndex: 'name' },
          { title: '值', dataIndex: 'data' },
          { title: '状态', dataIndex: 'enabled', width: 90 },
          { title: '操作', key: 'op', width: 80 },
        ]"
        row-key="id"
        size="small"
        :loading="loading"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.dataIndex === 'enabled'">
            <Switch
              :checked="record.enabled"
              size="small"
              @change="(v: any) => toggleOption(record.id, !!v)"
            />
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" class="mr-1" @click="editOpt(record as DhcpOptionRow)">编辑</Button>
            <Button size="small" danger @click="removeOption(record.id)">删除</Button>
          </template>
        </template>
      </Table>
    </Card>

    <Card title="类匹配规则（C-03，client-classes）">
      <div class="mb-3">
        <Button type="primary" size="small" @click="clsModalApi.open()">+ 添加类</Button>
      </div>
      <ClsModal class="w-[760px]">
        <div class="flex flex-wrap items-end gap-2">
          <div>
            <div class="mb-1 text-xs text-gray-400">类名</div>
            <Input v-model:value="clsForm.name" style="width: 150px" placeholder="printers" />
          </div>
          <div class="min-w-[260px] flex-1">
            <div class="mb-1 text-xs text-gray-400">test 表达式（Kea eval）</div>
            <Input v-model:value="clsForm.test" placeholder="option[61].hex == option[61].hex" />
          </div>
          <Button @click="addClassRow">+ 添加类内选项</Button>
          <Button type="primary" @click="addClass">{{ editingCls ? '保存修改' : '创建类' }}</Button>
          <Button v-if="editingCls" @click="cancelEditCls">取消编辑</Button>
        </div>
        <div v-for="(r, i) in clsForm.rows" :key="i" class="mt-2 flex flex-wrap items-center gap-2">
          <InputNumber v-model:value="r.optionCode" :min="1" :max="255" style="width: 90px" />
          <Input v-model:value="r.name" style="width: 160px" placeholder="routers" />
          <Input v-model:value="r.data" style="width: 180px" placeholder="192.168.9.254" />
          <Button size="small" danger @click="removeClassRow(i)">移除</Button>
        </div>
        <div class="mt-2 text-xs text-gray-400">
          命中 test 的客户端将收到该类的 option-data；类名是 Kea 引用键，创建后不可改。
        </div>
      </ClsModal>
      <Table
        :data-source="clsRows"
        :columns="[
          { title: '类名', dataIndex: 'name', width: 140 },
          { title: 'test 表达式', dataIndex: 'test' },
          { title: '下发选项', key: 'opts' },
          { title: '状态', dataIndex: 'enabled', width: 90 },
          { title: '操作', key: 'op', width: 80 },
        ]"
        row-key="id"
        size="small"
        :loading="loading"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'opts'">
            <span class="font-mono text-xs">{{ optsSummary(record.options) }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'enabled'">
            <Switch
              :checked="record.enabled"
              size="small"
              @change="(v: any) => toggleClass(record.id, !!v)"
            />
          </template>
          <template v-else-if="column.dataIndex === 'test'">
            <Tag class="font-mono">{{ record.test || '-' }}</Tag>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" class="mr-1" @click="editCls(record as DhcpClassRow)">编辑</Button>
            <Button size="small" danger @click="removeClass(record.id)">删除</Button>
          </template>
        </template>
      </Table>
    </Card>
  </div>
</template>