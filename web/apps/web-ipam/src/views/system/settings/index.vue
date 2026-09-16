<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';

import { updatePreferences } from '@vben/preferences';

import { Button, Card, Input, InputNumber, Popconfirm, Switch, Upload, message } from 'ant-design-vue';

import OrgManageCard from '#/components/org-manage-card.vue';

import { requestClient } from '#/api/request';
import {
  deleteLogArchive,
  downloadLogArchive,
  exportLogArchive,
  getLogSettings,
  getLogStorageStats,
  listLogArchives,
  updateLogSettings,
} from '#/api/ipam';
import type { LogArchive, LogSettings, LogStorageStats } from '#/api/ipam';

const form = reactive({ siteName: '', faviconUrl: '', logoUrl: '', serverPort: 8443 });
const restarting = ref(false);
const saving = ref(false);

// 侧栏 logo 渲染：preferences.logo 支持 URL（以 http/data 开头按图片渲染）
function apply() {
  if (form.siteName) document.title = form.siteName;
  if (form.faviconUrl) {
    let link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = form.faviconUrl;
  }
  // 同步侧栏：站点名称 + logo（页签/侧栏共用）
  updatePreferences({
    app: { name: form.siteName },
    logo: { source: form.logoUrl },
  });
}

async function load() {
  const v = await requestClient.get('/system/webui-settings');
  form.siteName = v.siteName ?? '';
  form.faviconUrl = v.faviconUrl ?? '';
  form.logoUrl = v.logoUrl ?? '';
  form.serverPort = Number(v.serverPort) || 8443;
  apply();
}

// 裁剪上传（adapter 增强 Upload：crop="true" + aspect-ratio="1:1" + max-size=2MB，
// 选中图片后自动弹 VCropper 裁剪，此处拿到 1:1 裁剪后的 Blob）
function onCropUpload(options: { file: Blob | File | string; onSuccess?: (body?: unknown) => void; onError?: (err: Error) => void }) {
  const reader = new FileReader();
  reader.onload = () => {
    form.faviconUrl = String(reader.result);
    form.logoUrl = form.faviconUrl;
    apply();
    options.onSuccess?.(options.file);
  };
  reader.onerror = () => options.onError?.(new Error('读取图片失败'));
  reader.readAsDataURL(options.file as Blob);
}

async function save() {
  if (!form.siteName.trim()) {
    message.warning('站点名称必填');
    return;
  }
  if (form.serverPort < 1 || form.serverPort > 65535) {
    message.warning('端口需在 1-65535 之间');
    return;
  }
  saving.value = true;
  try {
    await requestClient.put('/system/webui-settings', {
      siteName: form.siteName,
      faviconUrl: form.faviconUrl,
      logoUrl: form.logoUrl,
      serverPort: form.serverPort,
    });
    apply();
    message.success('已保存并应用（端口修改需重启后生效）');
  } finally {
    saving.value = false;
  }
}

async function restart() {
  restarting.value = true;
  try {
    await requestClient.post('/system/restart');
    message.success('已发起重启，页面将短暂不可用，请稍后刷新');
  } catch (e) {
    message.error(e instanceof Error ? e.message : '重启请求失败');
  } finally {
    restarting.value = false;
  }
}

const logForm = reactive<LogSettings>({ retentionDays: 180, archiveKeepMonths: 12, autoExport: true });
const logSaving = ref(false);
const stats = ref<LogStorageStats | null>(null);
const archives = ref<LogArchive[]>([]);
const exportMonthInput = ref('');
const exporting = ref(false);

function formatBytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatNumber(n: number): string {
  return (n ?? 0).toLocaleString();
}

async function loadLogSettings() {
  try {
    const s = await getLogSettings();
    logForm.retentionDays = s.retentionDays;
    logForm.archiveKeepMonths = s.archiveKeepMonths;
    logForm.autoExport = s.autoExport;
  } catch {
    // 策略读取失败不阻塞页面
  }
}

async function loadStats() {
  try {
    stats.value = await getLogStorageStats();
  } catch {
    stats.value = null;
  }
}

async function loadArchives() {
  try {
    archives.value = (await listLogArchives()).items ?? [];
  } catch {
    archives.value = [];
  }
}

async function saveLogSettings() {
  if (logForm.retentionDays < 1 || logForm.retentionDays > 3650) {
    message.warning('保留天数需在 1-3650 之间');
    return;
  }
  if (logForm.archiveKeepMonths < 1 || logForm.archiveKeepMonths > 120) {
    message.warning('归档保留月数需在 1-120 之间');
    return;
  }
  logSaving.value = true;
  try {
    await updateLogSettings({
      retentionDays: logForm.retentionDays,
      archiveKeepMonths: logForm.archiveKeepMonths,
      autoExport: logForm.autoExport,
    });
    message.success('已保存并生效');
    await loadStats();
  } catch (e) {
    message.error(e instanceof Error ? e.message : '保存失败');
  } finally {
    logSaving.value = false;
  }
}

async function doExport() {
  const m = exportMonthInput.value.trim();
  if (!/^\d{4}-\d{2}$/.test(m)) {
    message.warning('请输入 YYYY-MM 格式的月份');
    return;
  }
  exporting.value = true;
  try {
    await exportLogArchive(m);
    message.success(`已导出 ${m}`);
    await Promise.all([loadArchives(), loadStats()]);
  } catch (e) {
    message.error(e instanceof Error ? e.message : '导出失败');
  } finally {
    exporting.value = false;
  }
}

async function removeArchive(month: string) {
  try {
    await deleteLogArchive(month);
    message.success(`已删除 ${month} 归档`);
    await Promise.all([loadArchives(), loadStats()]);
  } catch (e) {
    message.error(e instanceof Error ? e.message : '删除失败');
  }
}

async function downloadArchive(month: string) {
  try {
    await downloadLogArchive(month);
  } catch (e) {
    message.error(e instanceof Error ? e.message : '下载失败');
  }
}

onMounted(() => {
  load();
  loadLogSettings();
  loadStats();
  loadArchives();
});
</script>

<template>
  <div class="p-4">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start">
      <div class="min-w-0 flex-1">
        <OrgManageCard />
      </div>
      <div class="min-w-0 flex-1">
        <Card title="Web 页面设置">
    <div class="space-y-3">
      <div>
        <div class="mb-1 text-xs text-gray-400">站点名称（浏览器页签名称/侧栏显示）</div>
        <Input v-model:value="form.siteName" placeholder="如 IPAM 管理平台" />
      </div>
      <div>
        <div class="mb-1 text-xs text-gray-400">站点图标（浏览器页签 + 侧栏共用，1:1 裁剪上传）</div>
        <div class="flex items-center gap-3">
          <Upload
            :show-upload-list="false"
            accept=".png,.jpg,.jpeg"
            :max-size="2"
            crop="true"
            aspect-ratio="1:1"
            list-type="picture-card"
            :custom-request="onCropUpload"
          >
            <div
              class="flex h-24 w-24 flex-col items-center justify-center overflow-hidden rounded border border-dashed border-gray-300 text-gray-400 transition-colors hover:border-primary hover:text-primary"
            >
              <img v-if="form.faviconUrl" :src="form.faviconUrl" alt="icon" class="h-full w-full object-contain" />
              <span v-else class="text-2xl leading-none">+</span>
            </div>
          </Upload>
          <div class="text-xs text-gray-400">
            支持 png / jpg / jpeg，≤2MB，按 1:1 裁剪后保存
            <Button v-if="form.faviconUrl" size="small" class="ml-2" @click="form.faviconUrl = ''; form.logoUrl = ''">
              清除
            </Button>
          </div>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <div class="mb-1 text-xs text-gray-400">服务端口</div>
        <InputNumber v-model:value="form.serverPort" :min="1" :max="65535" style="width: 160px" />
        <span class="text-xs text-gray-400">修改后需重启生效（容器部署请同步 .env 的 IPAM_PORT 并重建容器）</span>
      </div>
      <div class="flex items-center gap-2">
        <Button type="primary" :loading="saving" @click="save">保存并应用</Button>
        <Popconfirm
          title="确认重启服务？"
          description="重启后页面将短暂不可用；端口/设置修改在重启后生效。"
          ok-text="重启"
          cancel-text="取消"
          @confirm="restart"
        >
          <Button danger :loading="restarting">重启服务</Button>
        </Popconfirm>
      </div>
    </div>
        </Card>
      </div>
    </div>

    <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
      <Card title="日志存储策略">
        <div class="space-y-3">
          <div>
            <div class="mb-1 text-xs text-gray-400">日志保留天数（ClickHouse TTL，到期分区自动滚动删除）</div>
            <InputNumber v-model:value="logForm.retentionDays" :min="1" :max="3650" style="width: 160px" />
            <span class="ml-2 text-xs text-gray-400">默认 180 天</span>
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-400">归档保留月数（过期归档文件自动清理）</div>
            <InputNumber v-model:value="logForm.archiveKeepMonths" :min="1" :max="120" style="width: 160px" />
            <span class="ml-2 text-xs text-gray-400">默认 12 个月</span>
          </div>
          <div class="flex items-center gap-2">
            <Switch v-model:checked="logForm.autoExport" />
            <span class="text-xs text-gray-400">自动导出（每月 1 日 02:00 导出上月为 Parquet）</span>
          </div>
          <div>
            <Button type="primary" :loading="logSaving" @click="saveLogSettings">保存存储策略</Button>
          </div>
        </div>
      </Card>

      <Card title="日志存储概览" class="lg:col-span-2">
        <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <div>
            <div class="text-xs text-gray-400">总行数</div>
            <div class="text-xl font-semibold">{{ formatNumber(stats?.totalRows ?? 0) }}</div>
          </div>
          <div>
            <div class="text-xs text-gray-400">磁盘占用</div>
            <div class="text-xl font-semibold">{{ formatBytes(stats?.diskBytes ?? 0) }}</div>
          </div>
          <div>
            <div class="text-xs text-gray-400">最早月份</div>
            <div class="text-xl font-semibold">{{ stats?.earliestMonth || '—' }}</div>
          </div>
          <div>
            <div class="text-xs text-gray-400">当前 TTL</div>
            <div class="text-xl font-semibold">{{ stats?.retentionDays ?? logForm.retentionDays }} 天</div>
          </div>
        </div>
        <div v-if="stats?.partitions?.length" class="mt-4">
          <div class="mb-2 text-xs text-gray-400">按月分区（ClickHouse system.parts）</div>
          <div class="flex flex-wrap gap-2">
            <span
              v-for="p in stats.partitions"
              :key="p.month"
              class="rounded bg-gray-100 px-2 py-1 text-xs dark:bg-gray-800"
            >
              {{ p.month }} · {{ formatNumber(p.rowCount) }} 行 · {{ formatBytes(p.diskBytes) }}
            </span>
          </div>
        </div>
      </Card>
    </div>

    <Card title="日志归档（按月 Parquet）" class="mt-4">
      <div class="mb-3 flex flex-wrap items-center gap-2">
        <Input v-model:value="exportMonthInput" placeholder="YYYY-MM" style="width: 140px" />
        <Button type="primary" :loading="exporting" @click="doExport">导出该月</Button>
        <Button @click="loadArchives">刷新</Button>
        <span class="text-xs text-gray-400">导出为 Parquet 归档文件，ClickHouse 在线数据不受影响</span>
      </div>
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b text-left text-xs text-gray-400">
            <th class="py-2 font-normal">月份</th>
            <th class="font-normal">行数</th>
            <th class="font-normal">文件大小</th>
            <th class="font-normal">来源</th>
            <th class="font-normal">导出时间</th>
            <th class="text-right font-normal">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in archives" :key="a.month" class="border-b border-gray-100 dark:border-gray-800">
            <td class="py-2 font-mono">{{ a.month }}</td>
            <td>{{ formatNumber(a.rowCount) }}</td>
            <td>{{ formatBytes(a.fileSize) }}</td>
            <td>{{ a.exportedBy === 'system' ? '自动' : '手动' }}</td>
            <td>{{ new Date(a.createdAt).toLocaleString() }}</td>
            <td class="text-right">
              <Button size="small" @click="downloadArchive(a.month)">下载</Button>
              <Popconfirm
                title="确认删除该归档文件？"
                description="仅删除归档文件与记录，不影响 ClickHouse 在线数据。"
                ok-text="删除"
                cancel-text="取消"
                @confirm="removeArchive(a.month)"
              >
                <Button size="small" danger class="ml-2">删除</Button>
              </Popconfirm>
            </td>
          </tr>
          <tr v-if="archives.length === 0">
            <td colspan="6" class="py-6 text-center text-gray-400">暂无归档</td>
          </tr>
        </tbody>
      </table>
    </Card>
  </div>
</template>