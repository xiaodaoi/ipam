<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';

import { useUserStore } from '@vben/stores';

import {
  Alert,
  Button,
  Card,
  Input,
  message,
  Modal,
  Popconfirm,
  Tag,
} from 'ant-design-vue';

import {
  disableTotpApi,
  enableTotpApi,
  generateRecoveryCodesApi,
  getRecoveryStatusApi,
  logoutAllApi,
  setupTotpApi,
} from '#/api';
import {
  listSessions,
  revokeSession,
  type SessionRow,
} from '#/api/ipam';
import { useAuthStore } from '#/store';

const userStore = useUserStore();
const authStore = useAuthStore();

const enabled = computed(() => Boolean(userStore.userInfo?.totpEnabled));
const setup = ref<null | { otpauthUrl: string; secret: string }>(null);
const code = ref('');
const password = ref('');
const loading = ref(false);

// 恢复码（M6-007A）：生成时需再次确认身份，明文仅展示一次。
const recovery = reactive({
  open: false,
  codes: [] as string[],
  password: '',
  remaining: 0,
  totpCode: '',
});

async function loadRecovery() {
  if (!enabled.value) {
    recovery.remaining = 0;
    return;
  }
  try {
    const r = await getRecoveryStatusApi();
    recovery.remaining = r.remaining ?? 0;
  } catch {
    recovery.remaining = 0;
  }
}

function openRecovery() {
  recovery.password = '';
  recovery.totpCode = '';
  recovery.codes = [];
  recovery.open = true;
}

async function regenerateRecovery() {
  if (!recovery.password && !recovery.totpCode) {
    message.warning('请输入当前口令或 6 位动态口令');
    return;
  }
  loading.value = true;
  try {
    const r = await generateRecoveryCodesApi({
      password: recovery.password || undefined,
      totpCode: recovery.totpCode || undefined,
    });
    recovery.codes = r.codes ?? [];
    recovery.remaining = r.remaining ?? recovery.codes.length;
    message.success('已生成新的恢复码，旧恢复码立即失效');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '生成失败');
  } finally {
    loading.value = false;
  }
}

async function copyCodes() {
  try {
    await navigator.clipboard.writeText(recovery.codes.join('\n'));
    message.success('恢复码已复制');
  } catch {
    message.warning('复制失败，请手动选择文本复制');
  }
}

function downloadCodes() {
  const blob = new Blob(
    [
      `IPAM 恢复码（${userStore.userInfo?.username ?? ''}）\n生成时间：${new Date().toLocaleString()}\n每个恢复码仅可使用一次，请离线保存。\n\n${recovery.codes.join('\n')}\n`,
    ],
    { type: 'text/plain;charset=utf-8' },
  );
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'ipam-recovery-codes.txt';
  a.click();
  URL.revokeObjectURL(url);
}

watch(enabled, () => void loadRecovery(), { immediate: true });

async function generate() {
  loading.value = true;
  try {
    setup.value = await setupTotpApi();
    message.success('已生成密钥，请用认证器扫描/录入后输入动态口令确认');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '生成失败');
  } finally {
    loading.value = false;
  }
}

async function enable() {
  if (!/^\d{6}$/.test(code.value.trim())) {
    message.warning('请输入 6 位动态口令');
    return;
  }
  loading.value = true;
  try {
    await enableTotpApi(code.value.trim());
    if (userStore.userInfo) {
      userStore.setUserInfo({ ...userStore.userInfo, totpEnabled: true });
    }
    setup.value = null;
    code.value = '';
    message.success('双因素认证已启用');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '启用失败');
  } finally {
    loading.value = false;
  }
}

async function disable() {
  if (!password.value) {
    message.warning('请输入当前口令');
    return;
  }
  loading.value = true;
  try {
    await disableTotpApi(password.value);
    if (userStore.userInfo) {
      userStore.setUserInfo({ ...userStore.userInfo, totpEnabled: false });
    }
    password.value = '';
    message.success('双因素认证已关闭');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '关闭失败');
  } finally {
    loading.value = false;
  }
}

async function logoutAll() {
  try {
    await logoutAllApi();
    message.success('已注销全部会话，请重新登录');
    await authStore.logout();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '操作失败');
  }
}

// 登录会话（M6-007C）：查看当前账号在所有设备上的会话并单独下线。
const sessions = ref<SessionRow[]>([]);
const sessionsLoading = ref(false);

async function loadSessions() {
  sessionsLoading.value = true;
  try {
    const res = await listSessions();
    sessions.value = res.items ?? [];
  } catch (error) {
    message.error(error instanceof Error ? error.message : '会话列表加载失败');
  } finally {
    sessionsLoading.value = false;
  }
}

/** 从 User-Agent 提取可读设备名（未知时回退原始前缀）。 */
function deviceLabel(ua: string): string {
  if (!ua) return '未知客户端';
  let browser = '浏览器';
  if (/Edg\//.test(ua)) browser = 'Edge';
  else if (/Chrome\//.test(ua)) browser = 'Chrome';
  else if (/Firefox\//.test(ua)) browser = 'Firefox';
  else if (/Safari\//.test(ua)) browser = 'Safari';

  let os = '';
  if (/Windows/.test(ua)) os = 'Windows';
  else if (/Mac OS X|Macintosh/.test(ua)) os = 'macOS';
  else if (/Android/.test(ua)) os = 'Android';
  else if (/iPhone|iPad/.test(ua)) os = 'iOS';
  else if (/Linux/.test(ua)) os = 'Linux';
  return os ? `${browser} · ${os}` : browser;
}

function fmtTime(v: string): string {
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString();
}

async function kickSession(row: SessionRow) {
  try {
    await revokeSession(row.id);
    if (row.current) {
      message.success('当前会话已注销，请重新登录');
      await authStore.logout();
      return;
    }
    message.success('会话已强制下线');
    await loadSessions();
  } catch (error) {
    message.error(error instanceof Error ? error.message : '下线失败');
  }
}

onMounted(() => void loadSessions());
</script>

<template>
  <div class="flex flex-col gap-4">
    <Card>
      <template #title>
        <span class="ipam-card-title">
          <span>双因素认证（TOTP）</span>
          <Tag class="m-0" :color="enabled ? 'green' : 'default'">
            {{ enabled ? '已启用' : '未启用' }}
          </Tag>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">兼容 Google Authenticator / Microsoft Authenticator / 1Password</span>
      </template>

      <Alert
        v-if="enabled"
        class="mb-3"
        message="登录时需要输入认证器生成的 6 位动态口令；关闭前需验证当前口令。"
        show-icon
        type="success"
      />
      <Alert
        v-else
        class="mb-3"
        message="启用后登录需输入动态口令，可显著降低口令泄露风险。密钥仅展示一次，请妥善保存。"
        show-icon
        type="info"
      />

      <template v-if="!enabled">
        <div v-if="!setup" class="flex items-center gap-3">
          <Button type="primary" :loading="loading" @click="generate">
            生成 TOTP 密钥
          </Button>
        </div>
        <div v-else class="ipam-form-grid">
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">密钥（手动录入认证器）</span>
            <Input :value="setup.secret" readonly class="ipam-mono" />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">otpauth URI</span>
            <Input :value="setup.otpauthUrl" readonly class="ipam-mono" />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">动态口令（6 位）</span>
            <Input
              v-model:value="code"
              :maxlength="6"
              placeholder="输入认证器当前口令"
              @press-enter="enable"
            />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <div class="flex gap-2">
              <Button type="primary" :loading="loading" @click="enable">
                确认启用
              </Button>
              <Button @click="setup = null">取消</Button>
            </div>
          </div>
        </div>
      </template>

      <div v-else class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">当前口令（关闭时验证）</span>
          <Input.Password
            v-model:value="password"
            placeholder="输入当前登录口令"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <Button danger :loading="loading" @click="disable">关闭双因素认证</Button>
        </div>
      </div>
    </Card>

    <Card v-if="enabled">
      <template #title>
        <span class="ipam-card-title">
          恢复码
          <Tag class="m-0" :color="recovery.remaining > 0 ? 'blue' : 'orange'">
            剩余 {{ recovery.remaining }} 个
          </Tag>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">认证器丢失时用于登录，每个仅可使用一次</span>
      </template>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="ipam-hint max-w-xl">
          恢复码可替代 6 位动态口令登录。生成后旧码立即失效，明文仅展示一次；
          请打印或保存到离线位置，不要放在共享目录。
        </div>
        <Button :loading="loading" @click="openRecovery">
          {{ recovery.remaining > 0 ? '重新生成恢复码' : '生成恢复码' }}
        </Button>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          登录会话
          <Tag class="m-0" :color="sessions.length > 1 ? 'blue' : 'default'">
            {{ sessions.length }} 个
          </Tag>
        </span>
      </template>
      <template #extra>
        <Button size="small" :loading="sessionsLoading" @click="loadSessions">
          刷新
        </Button>
      </template>
      <div v-if="sessions.length === 0" class="ipam-hint">
        {{ sessionsLoading ? '加载中…' : '暂无活跃会话' }}
      </div>
      <div v-else class="flex flex-col gap-2">
        <div
          v-for="s in sessions"
          :key="s.id"
          class="flex flex-wrap items-center justify-between gap-3 rounded border border-gray-200 px-3 py-2 dark:border-gray-700"
        >
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <span class="font-medium">{{ deviceLabel(s.userAgent) }}</span>
              <Tag v-if="s.current" class="m-0" color="green">当前会话</Tag>
            </div>
            <div class="ipam-hint break-all">
              {{ s.ip || '未知 IP' }} · 登录 {{ fmtTime(s.createdAt) }} · 最近活动
              {{ fmtTime(s.lastSeenAt) }}
            </div>
          </div>
          <Popconfirm
            :title="s.current ? '确认注销当前会话？' : '确认强制下线该会话？'"
            description="该设备上的访问/刷新令牌将立即失效，需要重新登录。"
            ok-text="下线"
            cancel-text="取消"
            @confirm="kickSession(s)"
          >
            <Button danger size="small">下线</Button>
          </Popconfirm>
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">会话安全</span>
      </template>
      <template #extra>
        <span class="ipam-hint">注销后所有设备需重新登录</span>
      </template>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="ipam-hint max-w-xl">
          退出全部会话会提升当前账号的令牌版本，所有设备上的访问/刷新令牌立即失效；
          当前页面也会回到登录页。
        </div>
        <Popconfirm
          title="确认注销全部会话？"
          description="所有设备将立即退出登录，需要重新认证。"
          ok-text="注销"
          cancel-text="取消"
          @confirm="logoutAll"
        >
          <Button danger>注销全部会话</Button>
        </Popconfirm>
      </div>
    </Card>

    <Modal
      v-model:open="recovery.open"
      :footer="null"
      :title="recovery.codes.length > 0 ? '恢复码（仅展示一次）' : '生成恢复码'"
      :width="560"
    >
      <template v-if="recovery.codes.length === 0">
        <Alert
          class="mb-3"
          message="生成后，现有恢复码会立即失效"
          show-icon
          type="warning"
        />
        <div class="ipam-form-grid">
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">当前口令</span>
            <Input.Password
              v-model:value="recovery.password"
              placeholder="本地账号口令"
            />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">或动态口令</span>
            <Input
              v-model:value="recovery.totpCode"
              :maxlength="6"
              placeholder="认证器 6 位口令"
            />
          </div>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <Button @click="recovery.open = false">取消</Button>
          <Button type="primary" :loading="loading" @click="regenerateRecovery">
            生成
          </Button>
        </div>
      </template>
      <template v-else>
        <Alert
          class="mb-3"
          message="请立即保存：关闭后将无法再次查看这些恢复码"
          show-icon
          type="warning"
        />
        <div class="grid grid-cols-2 gap-2 text-sm">
          <span v-for="c in recovery.codes" :key="c" class="ipam-mono">
            {{ c }}
          </span>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <Button @click="copyCodes">复制全部</Button>
          <Button @click="downloadCodes">下载 TXT</Button>
          <Button type="primary" @click="recovery.open = false">
            我已保存
          </Button>
        </div>
      </template>
    </Modal>
  </div>
</template>
