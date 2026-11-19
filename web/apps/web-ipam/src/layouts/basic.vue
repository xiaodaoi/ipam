<script lang="ts" setup>
import type { NotificationItem } from '@vben/layouts';

import type { AlertRow } from '#/api/ipam';

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import { AuthenticationLoginExpiredModal } from '@vben/common-ui';
import { useWatermark } from '@vben/hooks';
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

function toNotification(a: AlertRow): NotificationItem {
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
      />
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
