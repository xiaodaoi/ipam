import { ref } from 'vue';

import { getLogSampling } from '#/api/ipam';

/**
 * 日志采样状态（M6-045）。
 *
 * 磁盘水位自动降级期间，内部日志按 1/N 抽样写入 ClickHouse，
 * 依赖日志的 KPI（DNS 查询量、拦截量、活跃地址趋势）都是**抽样下界**。
 * 页面用该组合式函数给出提示，避免把降级期间的数值当作全量。
 */
export function useLogSampling() {
  const degraded = ref(false);
  const diskPct = ref<number>();
  const rate = ref(1);

  async function load() {
    try {
      const s = await getLogSampling();
      degraded.value = Boolean(s.degraded);
      rate.value = Math.max(1, s.ingestSampleRate ?? 1);
      diskPct.value = s.diskUsedPercent;
    } catch {
      // 无权限或接口不可用时保持“未知”，不阻塞页面主流程。
    }
  }

  return { degraded, diskPct, load, rate };
}
