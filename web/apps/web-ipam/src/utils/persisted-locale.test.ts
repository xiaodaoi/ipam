import { describe, expect, it } from 'vitest';

import {
  localeStorageKey,
  parsePersistedLocale,
  readPersistedLocale,
} from './persisted-locale';

describe('localeStorageKey', () => {
  it('沿用上游 storage 命名规则', () => {
    expect(localeStorageKey('vben-web-antd-5.7.0-prod')).toBe(
      'vben-web-antd-5.7.0-prod-preferences-locale',
    );
  });
});

describe('parsePersistedLocale', () => {
  it('解析 StorageManager 的 { value } 包装', () => {
    expect(parsePersistedLocale('{"value":"en-US"}')).toBe('en-US');
    expect(parsePersistedLocale('{"value":"zh-CN"}')).toBe('zh-CN');
  });

  it('兼容裸字符串', () => {
    expect(parsePersistedLocale('"en-US"')).toBe('en-US');
  });

  it('拒绝空值、非法 JSON 与不支持的语言', () => {
    expect(parsePersistedLocale(null)).toBeUndefined();
    expect(parsePersistedLocale('')).toBeUndefined();
    expect(parsePersistedLocale('not-json')).toBeUndefined();
    expect(parsePersistedLocale('{"value":"fr-FR"}')).toBeUndefined();
    expect(parsePersistedLocale('{"value":42}')).toBeUndefined();
  });
});

describe('readPersistedLocale', () => {
  it('读取成功时返回语言', () => {
    expect(readPersistedLocale('k', () => '{"value":"en-US"}')).toBe('en-US');
  });

  it('storage 抛错时降级为 undefined', () => {
    expect(
      readPersistedLocale('k', () => {
        throw new Error('quota');
      }),
    ).toBeUndefined();
  });
});
