import type { OnboardingState } from '#/views/_core/onboarding/types';

import { useUserStore } from '@vben/stores';

const KEY_PREFIX = 'ipam.onboarding.v1.';

/** 首次向导状态按账号隔离存于浏览器（服务端无用户偏好表，换设备可重新显示）。 */
export function onboardingKey(username?: string): string {
  return `${KEY_PREFIX}${username || 'anonymous'}`;
}

export function readOnboarding(username?: string): OnboardingState {
  try {
    const raw = localStorage.getItem(onboardingKey(username));
    if (!raw) return {};
    const parsed = JSON.parse(raw) as OnboardingState;
    return typeof parsed === 'object' && parsed ? parsed : {};
  } catch {
    return {};
  }
}

export function writeOnboarding(
  username: string | undefined,
  patch: Partial<OnboardingState>,
): OnboardingState {
  const next = {
    ...readOnboarding(username),
    ...patch,
    updatedAt: new Date().toISOString(),
  };
  try {
    localStorage.setItem(onboardingKey(username), JSON.stringify(next));
  } catch {
    // 隐私模式/配额不足时静默降级：状态仅对当前页面有效。
  }
  return next;
}

/** 当前登录账号名（用于状态隔离）。 */
export function useOnboardingUsername(): string {
  const userStore = useUserStore();
  return userStore.userInfo?.username ?? 'anonymous';
}
