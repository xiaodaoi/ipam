/**
 * 语言偏好持久化辅助（M6-025）。
 *
 * 背景：上游 `@vben-core/preferences` 的 `initPreferences()` 用
 * `merge({}, initialPreferences, cachedPreferences)` 合并缓存，初始化配置
 * （含默认值 `app.locale = 'zh-CN'`）始终优先，缓存只补齐缺失字段。
 * 结果是 `updatePreferences({ app: { locale } })` 写入的 `preferences` 缓存
 * 在页面刷新后被默认值覆盖，语言回退为中文。
 *
 * 处理方式：`initPreferences()` 之前读取独立持久化的 `preferences-locale`
 * 键，初始化完成后再回填一次偏好设置，使语言选择在刷新后仍然生效。
 * 不修改上游包，仅在本应用入口做一次补偿。
 */
export type PersistedLocale = 'en-US' | 'zh-CN';

const SUPPORTED_LOCALES = new Set<string>(['en-US', 'zh-CN']);

/** 生成 `preferences-locale` 在指定命名空间下的存储键。 */
export function localeStorageKey(namespace: string): string {
  return `${namespace}-preferences-locale`;
}

/** 解析 `preferences-locale` 的原始值，非法值返回 undefined。 */
export function parsePersistedLocale(
  raw: null | string,
): PersistedLocale | undefined {
  if (!raw) return undefined;
  try {
    const parsed: unknown = JSON.parse(raw);
    const value =
      parsed && typeof parsed === 'object' && 'value' in parsed
        ? (parsed as { value: unknown }).value
        : parsed;
    return typeof value === 'string' && SUPPORTED_LOCALES.has(value)
      ? (value as PersistedLocale)
      : undefined;
  } catch {
    return undefined;
  }
}

/** 从 storage 读取上次选择的语言；读取失败时返回 undefined。 */
export function readPersistedLocale(
  storageKey: string,
  getItem: (key: string) => null | string = (key) =>
    globalThis.localStorage?.getItem(key) ?? null,
): PersistedLocale | undefined {
  try {
    return parsePersistedLocale(getItem(storageKey));
  } catch {
    return undefined;
  }
}
