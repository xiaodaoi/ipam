import { updatePreferences } from '@vben/preferences';

import { requestClient } from '#/api/request';

export interface SiteBranding {
  siteName: string;
  faviconUrl: string;
  logoUrl: string;
}

/**
 * 站点名单字图标（SVG data URI）。
 *
 * vben 折叠侧栏只渲染 logo 图片（文本被 `v-if="!collapsed"` 隐藏），未上传 logo 时
 * 该区域会彻底空白；以站点名首字生成占位图标，折叠态即可见（如「DDI」→「D」）。
 */
export function monogramLogo(text: string): string {
  const ch = Array.from(text.trim())[0] ?? '?';
  const svg =
    '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">' +
    '<rect width="64" height="64" rx="14" fill="#1677ff"/>' +
    `<text x="32" y="44" font-family="-apple-system,'Segoe UI',Roboto,sans-serif" font-size="38" font-weight="600" fill="#ffffff" text-anchor="middle">${ch}</text>` +
    '</svg>';
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
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
  const mark = v.siteName ? monogramLogo(v.siteName) : '';
  if (v.siteName) {
    document.title = v.siteName;
  }
  const favicon = v.faviconUrl || mark;
  if (favicon) {
    let link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = favicon;
  }
  const next: { app?: { name: string }; logo?: { source: string } } = {};
  if (v.siteName) {
    next.app = { name: v.siteName };
  }
  const logoSource = v.logoUrl || mark;
  if (logoSource) {
    next.logo = { source: logoSource };
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
