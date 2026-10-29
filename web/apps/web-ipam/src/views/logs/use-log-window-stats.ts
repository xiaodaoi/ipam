import type { LogQuery } from '#/api/ipam';

import { ref } from 'vue';

import { getLogQps, listLogs } from '#/api/ipam';

/** 统计窗口（页面持有快照，避免查询过程中时间漂移） */
export interface LogRange {
  from: string;
  to?: string;
}

export interface LogWindowStats {
  /** 封禁拦截（action=blocked，属于 DNS 子集） */
  blocked: number;
  /** QPS 曲线分桶数 */
  buckets: number;
  /** DHCP 事件数 */
  dhcp: number;
  /** DNS 查询数 */
  dns: number;
  /** 趋势形状（仅数值） */
  spark: number[];
  /** 窗口内事件总量（QPS 分桶求和，与 count 口径一致） */
  total: number;
}

/**
 * 日志时间窗统计（M4-020）：
 * 一次 QPS 曲线 + 三次轻量 count 查询，供检索页与概览页复用。
 * 查询失败时保留 failed 标记，由页面降级为「—」而不是误导性的 0。
 */
export function useLogWindowStats() {
  const stats = ref<LogWindowStats>();
  const loading = ref(false);
  const failed = ref(false);
  let seq = 0;

  function bucketSeconds(r: LogRange): number {
    const start = Date.parse(r.from);
    const end = r.to ? Date.parse(r.to) : Date.now();
    const span = Math.max(60, (end - start) / 1000);
    return Math.max(60, Math.round(span / 48));
  }

  async function load(r: LogRange) {
    const my = ++seq;
    loading.value = true;
    failed.value = false;
    try {
      const base: LogQuery = { from: r.from, to: r.to };
      const [qps, dns, dhcp, blocked] = await Promise.all([
        getLogQps({ ...base, intervalSec: bucketSeconds(r) }),
        listLogs({ ...base, type: 'dns', pageSize: 1 }),
        listLogs({ ...base, type: 'dhcp', pageSize: 1 }),
        listLogs({ ...base, action: 'blocked', pageSize: 1 }),
      ]);
      if (my !== seq) return;
      const spark = (qps.points ?? []).map((p) => p.count);
      stats.value = {
        spark,
        total: spark.reduce((sum, v) => sum + v, 0),
        dns: dns.total ?? 0,
        dhcp: dhcp.total ?? 0,
        blocked: blocked.total ?? 0,
        buckets: spark.length,
      };
    } catch {
      if (my === seq) {
        stats.value = undefined;
        failed.value = true;
      }
    } finally {
      if (my === seq) loading.value = false;
    }
  }

  return { failed, load, loading, stats };
}
