import { updatePreferences } from '@vben/preferences';

import { requestClient } from '#/api/request';

export interface SiteBranding {
  siteName: string;
  faviconUrl: string;
  logoUrl: string;
}

/**
 * 应用站点品牌（浏览器标题 / 页签图标 / 侧栏 logo）。
 *
 * 必须运行时重新应用：vben 的 preferences 初始化用 defu（首参优先），
 * `overrides ∪ defaults` 会永久压过 localStorage 缓存——而站点名来自服务端
 * webui_settings 表，因此不重新应用的话，刷新后会被 VITE_APP_TITLE 覆盖回去。
 * 标题由 bootstrap 的 watchEffect 从 preferences.app.name 响应式推导，这里
 * 只需更新 preferences 即可联动。
 */
export function applyBranding(v: Partial<SiteBranding>): void {
  if (v.siteName) {
    document.title = v.siteName;
  }
  if (v.faviconUrl) {
    let link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = v.faviconUrl;
  }
  const next: { app?: { name: string }; logo?: { source: string } } = {};
  if (v.siteName) {
    next.app = { name: v.siteName };
  }
  if (v.logoUrl !== undefined) {
    next.logo = { source: v.logoUrl };
  }
  if (Object.keys(next).length > 0) {
    updatePreferences(next);
  }
}

/** 拉取站点品牌并应用；失败静默（不阻塞启动）。
 * 走免鉴权只读端点 /public/branding，故登录页（鉴权前）同样生效。 */
export async function applySiteBranding(): Promise<null | SiteBranding> {
  try {
    const v = await requestClient.get<SiteBranding>('/public/branding');
    applyBranding(v);
    return v;
  } catch {
    return null;
  }
}
