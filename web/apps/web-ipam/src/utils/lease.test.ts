import { describe, expect, it } from 'vitest';

import { filterLeases, formatLeaseRemaining } from './lease';

describe('formatLeaseRemaining', () => {
  const now = 1_800_000_000;
  it('按剩余秒数分级展示', () => {
    expect(formatLeaseRemaining({}, now)).toBe('—');
    expect(formatLeaseRemaining({ cltt: now - 100, validLifetime: 60 }, now)).toBe('已过期');
    expect(formatLeaseRemaining({ cltt: now, validLifetime: 30 }, now)).toBe('不足 1 分钟');
    expect(formatLeaseRemaining({ cltt: now, validLifetime: 300 }, now)).toBe('5 分钟');
    expect(formatLeaseRemaining({ cltt: now, validLifetime: 7200 }, now)).toBe('2.0 小时');
    expect(formatLeaseRemaining({ cltt: now, validLifetime: 172800 }, now)).toBe('2.0 天');
  });
});

describe('filterLeases', () => {
  const rows = [
    { ipAddress: '10.0.0.5', hwAddress: '02:00:00:00:00:AA', hostname: 'Host-A', subnetId: 3 },
    { ipAddress: 'fd00::1', duid: '00:01:aa', leaseType: 'IA_NA' },
  ];
  it('空关键字返回全部', () => {
    expect(filterLeases(rows, '  ')).toHaveLength(2);
  });
  it('按 IP/MAC/主机名/DUID/子网匹配且不区分大小写', () => {
    expect(filterLeases(rows, '10.0.0')).toHaveLength(1);
    expect(filterLeases(rows, '00:aa')).toHaveLength(1);
    expect(filterLeases(rows, 'host-a')).toHaveLength(1);
    expect(filterLeases(rows, '00:01:aa')).toHaveLength(1);
    expect(filterLeases(rows, '3')).toHaveLength(1);
    expect(filterLeases(rows, 'none')).toHaveLength(0);
  });
});
