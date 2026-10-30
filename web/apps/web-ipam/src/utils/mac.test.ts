import { describe, expect, it } from 'vitest';

import { normalizeMacInput } from './mac';

describe('normalizeMacInput', () => {
  it('兼容常见 MAC 写法', () => {
    expect(normalizeMacInput('C4-3D-1A-07-EB-2B')).toBe('c4:3d:1a:07:eb:2b');
    expect(normalizeMacInput('C43D1A07EB2B')).toBe('c4:3d:1a:07:eb:2b');
    expect(normalizeMacInput('c4.3d.1a.07.eb.2b')).toBe('c4:3d:1a:07:eb:2b');
  });

  it('拒绝非法输入', () => {
    expect(normalizeMacInput('')).toBeNull();
    expect(normalizeMacInput('C4:3D:1A:07:EB')).toBeNull();
    expect(normalizeMacInput('zz:3d:1a:07:eb:2b')).toBeNull();
  });
});
