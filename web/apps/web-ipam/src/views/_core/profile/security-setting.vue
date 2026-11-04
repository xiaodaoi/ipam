<script setup lang="ts">
import type { SessionRow } from '#/api/ipam';

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
import { listSessions, revokeSession } from '#/api/ipam';
import { $t } from '#/locales';
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
    message.warning($t('core.profile.security.needPasswordOrCode'));
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
    message.success($t('core.profile.security.codeGenerated'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.generateFail'),
    );
  } finally {
    loading.value = false;
  }
}

async function copyCodes() {
  try {
    await navigator.clipboard.writeText(recovery.codes.join('\n'));
    message.success($t('core.profile.security.copied'));
  } catch {
    message.warning($t('core.profile.security.copyFail'));
  }
}

function downloadCodes() {
  const blob = new Blob(
    [
      `${$t('core.profile.security.downloadHeader', {
        user: userStore.userInfo?.username ?? '',
        time: new Date().toLocaleString(),
      })}${recovery.codes.join('\n')}\n`,
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
    message.success($t('core.profile.security.totpEnabledMsg'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.generateFail'),
    );
  } finally {
    loading.value = false;
  }
}

async function enable() {
  if (!/^\d{6}$/.test(code.value.trim())) {
    message.warning($t('core.profile.security.needCode'));
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
    message.success($t('core.profile.security.totpOn'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.enableFail'),
    );
  } finally {
    loading.value = false;
  }
}

async function disable() {
  if (!password.value) {
    message.warning($t('core.profile.security.needCurrentPassword'));
    return;
  }
  loading.value = true;
  try {
    await disableTotpApi(password.value);
    if (userStore.userInfo) {
      userStore.setUserInfo({ ...userStore.userInfo, totpEnabled: false });
    }
    password.value = '';
    message.success($t('core.profile.security.totpOff'));
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.disableFail'),
    );
  } finally {
    loading.value = false;
  }
}

async function logoutAll() {
  try {
    await logoutAllApi();
    message.success($t('core.profile.security.logoutAllMsg'));
    await authStore.logout();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.opFail'),
    );
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
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.sessionLoadFail'),
    );
  } finally {
    sessionsLoading.value = false;
  }
}

/** 从 User-Agent 提取可读设备名（未知时回退原始前缀）。 */
function deviceLabel(ua: string): string {
  if (!ua) return $t('core.profile.security.unknownClient');
  let browser = $t('core.profile.security.browser');
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
      message.success($t('core.profile.security.kickCurrentOk'));
      await authStore.logout();
      return;
    }
    message.success($t('core.profile.security.kickOk'));
    await loadSessions();
  } catch (error) {
    message.error(
      error instanceof Error
        ? error.message
        : $t('core.profile.security.kickFail'),
    );
  }
}

onMounted(() => void loadSessions());
</script>

<template>
  <div class="flex flex-col gap-4">
    <Card>
      <template #title>
        <span class="ipam-card-title">
          <span>{{ $t('core.profile.security.totpTitle') }}</span>
          <Tag class="m-0" :color="enabled ? 'green' : 'default'">
            {{
              enabled
                ? $t('core.profile.security.enabled')
                : $t('core.profile.security.disabled')
            }}
          </Tag>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{ $t('core.profile.security.compat') }}</span>
      </template>

      <Alert
        v-if="enabled"
        class="mb-3"
        :message="$t('core.profile.security.alertEnabled')"
        show-icon
        type="success"
      />
      <Alert
        v-else
        class="mb-3"
        :message="$t('core.profile.security.alertDisabled')"
        show-icon
        type="info"
      />

      <template v-if="!enabled">
        <div v-if="!setup" class="flex items-center gap-3">
          <Button type="primary" :loading="loading" @click="generate">
            {{ $t('core.profile.security.generate') }}
          </Button>
        </div>
        <div v-else class="ipam-form-grid">
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">{{
              $t('core.profile.security.secretLabel')
            }}</span>
            <Input :value="setup.secret" readonly class="ipam-mono" />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <span class="ipam-form-field__label">{{
              $t('core.profile.security.otpauthLabel')
            }}</span>
            <Input :value="setup.otpauthUrl" readonly class="ipam-mono" />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{
              $t('core.profile.security.totpLabel')
            }}</span>
            <Input
              v-model:value="code"
              :maxlength="6"
              :placeholder="$t('core.profile.security.totpPlaceholder')"
              @press-enter="enable"
            />
          </div>
          <div class="ipam-form-field ipam-form-field--wide">
            <div class="flex gap-2">
              <Button type="primary" :loading="loading" @click="enable">
                {{ $t('core.profile.security.confirmEnable') }}
              </Button>
              <Button @click="setup = null">
                {{ $t('core.profile.security.cancel') }}
              </Button>
            </div>
          </div>
        </div>
      </template>

      <div v-else class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">
            {{ $t('core.profile.security.currentLabel') }}
          </span>
          <Input.Password
            v-model:value="password"
            :placeholder="$t('core.profile.security.currentPlaceholder')"
          />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <Button danger :loading="loading" @click="disable">
            {{ $t('core.profile.security.disable') }}
          </Button>
        </div>
      </div>
    </Card>

    <Card v-if="enabled">
      <template #title>
        <span class="ipam-card-title">
          {{ $t('core.profile.security.recoveryTitle') }}
          <Tag class="m-0" :color="recovery.remaining > 0 ? 'blue' : 'orange'">
            {{
              $t('core.profile.security.remaining', { n: recovery.remaining })
            }}
          </Tag>
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{
          $t('core.profile.security.recoveryHint')
        }}</span>
      </template>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="ipam-hint max-w-xl">
          {{ $t('core.profile.security.recoveryDesc') }}
        </div>
        <Button :loading="loading" @click="openRecovery">
          {{
            recovery.remaining > 0
              ? $t('core.profile.security.regen')
              : $t('core.profile.security.genRecovery')
          }}
        </Button>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          {{ $t('core.profile.security.sessionTitle') }}
          <Tag class="m-0" :color="sessions.length > 1 ? 'blue' : 'default'">
            {{
              $t('core.profile.security.sessionCount', { n: sessions.length })
            }}
          </Tag>
        </span>
      </template>
      <template #extra>
        <Button size="small" :loading="sessionsLoading" @click="loadSessions">
          {{ $t('core.profile.security.refresh') }}
        </Button>
      </template>
      <div v-if="sessions.length === 0" class="ipam-hint">
        {{
          sessionsLoading
            ? $t('core.profile.security.loading')
            : $t('core.profile.security.noSessions')
        }}
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
              <Tag v-if="s.current" class="m-0" color="green">
                {{ $t('core.profile.security.currentSession') }}
              </Tag>
            </div>
            <div class="ipam-hint break-all">
              {{
                $t('core.profile.security.sessionMeta', {
                  ip: s.ip || $t('core.profile.security.unknownIp'),
                  login: fmtTime(s.createdAt),
                  seen: fmtTime(s.lastSeenAt),
                })
              }}
            </div>
          </div>
          <Popconfirm
            :title="
              s.current
                ? $t('core.profile.security.kickCurrentTitle')
                : $t('core.profile.security.kickTitle')
            "
            :description="$t('core.profile.security.kickDesc')"
            :ok-text="$t('core.profile.security.kick')"
            :cancel-text="$t('core.profile.security.cancel')"
            @confirm="kickSession(s)"
          >
            <Button danger size="small">
              {{ $t('core.profile.security.kick') }}
            </Button>
          </Popconfirm>
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          {{ $t('core.profile.security.securityTitle') }}
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">{{
          $t('core.profile.security.securityHint')
        }}</span>
      </template>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="ipam-hint max-w-xl">
          {{ $t('core.profile.security.securityDesc') }}
        </div>
        <Popconfirm
          :title="$t('core.profile.security.logoutAllTitle')"
          :description="$t('core.profile.security.logoutAllDesc')"
          :ok-text="$t('core.profile.security.logoutAllOk')"
          :cancel-text="$t('core.profile.security.cancel')"
          @confirm="logoutAll"
        >
          <Button danger>{{ $t('core.profile.security.logoutAll') }}</Button>
        </Popconfirm>
      </div>
    </Card>

    <Modal
      v-model:open="recovery.open"
      :footer="null"
      :title="
        recovery.codes.length > 0
          ? $t('core.profile.security.recoveryShowTitle')
          : $t('core.profile.security.recoveryGenTitle')
      "
      :width="560"
    >
      <template v-if="recovery.codes.length === 0">
        <Alert
          class="mb-3"
          :message="$t('core.profile.security.regenWarn')"
          show-icon
          type="warning"
        />
        <div class="ipam-form-grid">
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{
              $t('core.profile.security.recoveryPwdLabel')
            }}</span>
            <Input.Password
              v-model:value="recovery.password"
              :placeholder="$t('core.profile.security.recoveryPwdPlaceholder')"
            />
          </div>
          <div class="ipam-form-field">
            <span class="ipam-form-field__label">{{
              $t('core.profile.security.recoveryTotpLabel')
            }}</span>
            <Input
              v-model:value="recovery.totpCode"
              :maxlength="6"
              :placeholder="$t('core.profile.security.recoveryTotpPlaceholder')"
            />
          </div>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <Button @click="recovery.open = false">
            {{ $t('core.profile.security.cancel') }}
          </Button>
          <Button type="primary" :loading="loading" @click="regenerateRecovery">
            {{ $t('core.profile.security.generateBtn') }}
          </Button>
        </div>
      </template>
      <template v-else>
        <Alert
          class="mb-3"
          :message="$t('core.profile.security.saveWarn')"
          show-icon
          type="warning"
        />
        <div class="grid grid-cols-2 gap-2 text-sm">
          <span v-for="c in recovery.codes" :key="c" class="ipam-mono">
            {{ c }}
          </span>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <Button @click="copyCodes">
            {{ $t('core.profile.security.copyAll') }}
          </Button>
          <Button @click="downloadCodes">
            {{ $t('core.profile.security.downloadTxt') }}
          </Button>
          <Button type="primary" @click="recovery.open = false">
            {{ $t('core.profile.security.saved') }}
          </Button>
        </div>
      </template>
    </Modal>
  </div>
</template>
