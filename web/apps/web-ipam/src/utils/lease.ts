export interface LeaseLike {
  cltt?: number;
  duid?: string;
  hostname?: string;
  hwAddress?: string;
  ipAddress: string;
  leaseType?: string;
  subnetId?: number;
  validLifetime?: number;
}

/** 剩余租期的人类可读文本（cltt + valid-lft - now）。 */
export function formatLeaseRemaining(
  lease: Pick<LeaseLike, 'cltt' | 'validLifetime'>,
  now = Math.floor(Date.now() / 1000),
): string {
  if (!lease.cltt || !lease.validLifetime) return '—';
  const left = lease.cltt + lease.validLifetime - now;
  if (left <= 0) return '已过期';
  if (left < 60) return '不足 1 分钟';
  if (left < 60 * 60) return `${Math.ceil(left / 60)} 分钟`;
  if (left < 24 * 60 * 60) return `${(left / 3600).toFixed(1)} 小时`;
  return `${(left / (24 * 3600)).toFixed(1)} 天`;
}

/** 租约关键字过滤（IP/MAC/主机名/DUID/Kea 子网）。 */
export function filterLeases<T extends LeaseLike>(rows: T[], keyword: string): T[] {
  const kw = keyword.trim().toLowerCase();
  if (!kw) return rows;
  return rows.filter((r) =>
    [r.ipAddress, r.hwAddress, r.hostname, r.duid, String(r.subnetId ?? '')]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(kw)),
  );
}
