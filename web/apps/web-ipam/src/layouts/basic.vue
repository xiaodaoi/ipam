<script lang="ts" setup>
import type { NotificationItem } from '@vben/layouts';

import type { AlertRow } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import { AuthenticationLoginExpiredModal } from '@vben/common-ui';
import { useWatermark } from '@vben/hooks';
import { CircleCheckBig, CircleX, IconifyIcon } from '@vben/icons';
import {
  BasicLayout,
  LockScreen,
  Notification,
  UserDropdown,
} from '@vben/layouts';
import { preferences, usePreferences } from '@vben/preferences';
import { useAccessStore, useUserStore } from '@vben/stores';

import { ackAlerts, ackAllAlerts, getAlertUnread } from '#/api/ipam';
import { $t } from '#/locales';
import { useAuthStore } from '#/store';
import LoginForm from '#/views/_core/authentication/login.vue';

// 报警栏（M6-040）：轮询未确认报警；点击通知即确认并跳转报警详情。
const notifications = ref<NotificationItem[]>([]);
const unreadCount = ref(0);
let pollTimer: ReturnType<typeof setInterval> | undefined;

function fmtAlertTime(v?: string): string {
  const t = v ? Date.parse(v) : Number.NaN;
  return Number.isNaN(t) || t <= 0
    ? ''
    : new Date(t).toLocaleString(undefined, { hour12: false });
}

function alertText(key: string, fallback: string): string {
  const text = $t(key);
  return text === key ? fallback : text;
}

function alertTitle(a: AlertRow): string {
  if (a.source === 'dns' && a.domain) {
    return $t('logs.alerts.notify.riskTitle', { domain: a.domain });
  }
  return alertText(`logs.alerts.kinds.${a.kind}`, a.title);
}

function alertMessage(a: AlertRow): string {
  if (a.source === 'dns') {
    return $t('logs.alerts.notify.riskMessage', {
      ip: a.clientIp || '—',
      n: a.count,
    });
  }
  const component = a.component
    ? alertText(`logs.alerts.components.${a.component}`, a.component)
    : '—';
  return $t('logs.alerts.notify.systemMessage', { component, n: a.count });
}

function noticeIcon(a: Record<string, any>): string {
  if (a.source === 'dns') return 'lucide:shield-ban';
  return a.severity === 'critical'
    ? 'lucide:octagon-alert'
    : 'lucide:triangle-alert';
}

function noticeIconClass(severity?: string): string {
  switch (severity) {
    case 'critical': {
      return 'bg-red-500/10 text-red-500';
    }
    case 'warning': {
      return 'bg-amber-500/10 text-amber-500';
    }
    default: {
      return 'bg-blue-500/10 text-blue-500';
    }
  }
}

function toNotification(a: AlertRow): NotificationItem {
  const sourceName = (a.intelSourceName ?? '')
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)
    .join(' / ');
  return {
    id: a.id,
    avatar: '/avatar.svg',
    date: fmtAlertTime(a.lastSeen),
    isRead: false,
    title: alertTitle(a),
    message: alertMessage(a),
    link: '/logs-center/alerts',
    query: { alertId: a.id },
    severity: a.severity,
    source: a.source,
    sourceName,
  };
}

async function loadNotifications(): Promise<void> {
  try {
    const res = await getAlertUnread(8);
    unreadCount.value = res.count ?? 0;
    notifications.value = (res.items ?? []).map((a) => toNotification(a));
  } catch {
    // 未登录/网络抖动：静默失败，下个周期再试（不阻塞布局）。
  }
}

async function ackById(id: number | string): Promise<void> {
  const key = String(id);
  try {
    await ackAlerts([key]);
  } catch {
    // 确认失败仍移除本地项，避免重复打扰；下个轮询会以服务端为准。
  }
  notifications.value = notifications.value.filter((n) => n.id !== id);
  unreadCount.value = Math.max(0, unreadCount.value - 1);
}

onMounted(() => {
  void loadNotifications();
  pollTimer = setInterval(() => void loadNotifications(), 30_000);
});
onBeforeUnmount(() => pollTimer && clearInterval(pollTimer));

const router = useRouter();
const userStore = useUserStore();
const authStore = useAuthStore();
const accessStore = useAccessStore();
const { destroyWatermark, updateWatermark } = useWatermark();
const { isDark } = usePreferences();
const showDot = computed(
  () =>
    unreadCount.value > 0 || notifications.value.some((item) => !item.isRead),
);

const menus = computed(() => [
  {
    handler: () => {
      router.push({ name: 'Profile' });
    },
    icon: 'lucide:user',
    text: $t('page.auth.profile'),
  },
]);

const avatar = computed(() => {
  return userStore.userInfo?.avatar ?? preferences.app.defaultAvatar;
});

async function handleLogout() {
  await authStore.logout(false);
}

function handleNoticeClear() {
  // 报警栏以服务端未确认集合为准：清除即全部确认，避免下个轮询又出现。
  notifications.value = [];
  unreadCount.value = 0;
  void ackAllAlerts().catch(() => undefined);
}

function markRead(id: number | string) {
  void ackById(id);
}

function remove(id: number | string) {
  notifications.value = notifications.value.filter((item) => item.id !== id);
}

function handleMakeAll() {
  notifications.value = [];
  unreadCount.value = 0;
  void ackAllAlerts().catch(() => undefined);
}

const viewAll = () => {
  void router.push({ path: '/logs-center/alerts' });
};

const handleClick = (item: NotificationItem) => {
  // 查看即确认：先确认，再跳转到报警详情。
  void ackById(item.id);
  if (item.link) {
    navigateTo(item.link, item.query, item.state);
  }
};

function navigateTo(
  link: string,
  query?: Record<string, any>,
  state?: Record<string, any>,
) {
  if (link.startsWith('http://') || link.startsWith('https://')) {
    // 外部链接，在新标签页打开
    window.open(link, '_blank');
  } else {
    // 内部路由链接，支持 query 参数和 state
    router.push({
      path: link,
      query: query || {},
      state,
    });
  }
}

watch(
  () => ({
    enable: preferences.app.watermark,
    content: preferences.app.watermarkContent,
    isDark: isDark.value,
  }),
  async ({ enable, content, isDark: isDarkValue }) => {
    if (enable) {
      const watermarkColor = isDarkValue
        ? 'rgba(255, 255, 255, 0.12)'
        : 'rgba(0, 0, 0, 0.12)';

      await updateWatermark({
        advancedStyle: {
          colorStops: [
            {
              color: watermarkColor,
              offset: 0,
            },
            {
              color: watermarkColor,
              offset: 1,
            },
          ],
          type: 'linear',
        },
        content:
          content ||
          `${userStore.userInfo?.username} - ${userStore.userInfo?.realName}`,
      });
    } else {
      destroyWatermark();
    }
  },
  {
    immediate: true,
  },
);
</script>

<template>
  <BasicLayout @clear-preferences-and-logout="handleLogout">
    <template #user-dropdown>
      <UserDropdown
        :avatar
        :menus
        :text="userStore.userInfo?.realName"
        :description="userStore.userInfo?.username"
        @logout="handleLogout"
        @clear-preferences-and-logout="handleLogout"
      />
    </template>
    <template #notification>
      <Notification
        :dot="showDot"
        :notifications="notifications"
        @clear="handleNoticeClear"
        @read="(item) => item.id && markRead(item.id)"
        @remove="(item) => item.id && remove(item.id)"
        @make-all="handleMakeAll"
        @on-click="handleClick"
        @view-all="viewAll"
      >
        <template #content="{ item }">
          <span
            v-if="!item.isRead"
            class="absolute top-2 right-2 size-2 rounded-sm bg-primary"
          ></span>
          <span
            class="relative flex size-9 shrink-0 items-center justify-center rounded-full"
            :class="noticeIconClass(item.severity)"
          >
            <IconifyIcon :icon="noticeIcon(item)" class="size-4" />
          </span>
          <div class="min-w-0 flex-1 pr-9">
            <p class="line-clamp-2 font-semibold" :title="item.title">
              {{ item.title }}
            </p>
            <p class="mt-1 line-clamp-2 text-xs text-muted-foreground">
              {{ item.message }}
            </p>
            <p
              v-if="item.sourceName"
              class="mt-1 flex items-center gap-1 text-xs text-muted-foreground"
            >
              <IconifyIcon icon="lucide:radar" class="size-3 shrink-0" />
              <span class="truncate">
                {{
                  $t('logs.alerts.notify.riskSource', {
                    source: item.sourceName,
                  })
                }}
              </span>
            </p>
            <p class="mt-1 text-[11px] text-muted-foreground/70">
              {{ item.date }}
            </p>
          </div>
          <div class="absolute top-1/2 right-2 flex -translate-y-1/2">
            <button
              v-if="!item.isRead"
              type="button"
              class="notice-action"
              :title="$t('common.confirm')"
              @click.stop="item.id && markRead(item.id)"
            >
              <CircleCheckBig class="size-4" />
            </button>
            <button
              v-else
              type="button"
              class="notice-action notice-action--danger"
              :title="$t('common.delete')"
              @click.stop="item.id && remove(item.id)"
            >
              <CircleX class="size-4" />
            </button>
          </div>
        </template>
      </Notification>
    </template>
    <template #extra>
      <AuthenticationLoginExpiredModal
        v-model:open="accessStore.loginExpired"
        :avatar
      >
        <LoginForm />
      </AuthenticationLoginExpiredModal>
    </template>
    <template #lock-screen>
      <LockScreen :avatar @to-login="handleLogout" />
    </template>
  </BasicLayout>
</template>

<style scoped>
.notice-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  color: hsl(var(--muted-foreground));
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;
  transition:
    color 0.15s ease,
    background-color 0.15s ease;
}

.notice-action:hover {
  color: hsl(var(--foreground));
  background: hsl(var(--accent));
}

.notice-action--danger:hover {
  color: hsl(var(--destructive));
}
</style>
