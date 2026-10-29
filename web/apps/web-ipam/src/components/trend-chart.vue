<script setup lang="ts">
import type { CSSProperties } from 'vue';

import { computed, ref, useId } from 'vue';

import { useElementSize } from '@vueuse/core';
import dayjs from 'dayjs';

type Tone = 'amber' | 'blue' | 'cyan' | 'green' | 'violet';

const props = withDefaults(
  defineProps<{
    /** 是否填充面积 */
    area?: boolean;
    /** 柱状模式（默认折线） */
    bars?: boolean;
    /** 数据点 */
    data: { ts: string; value: number }[];
    /** 图表高度（px） */
    height?: number;
    /** 是否绘制均值/峰值参考线 */
    markers?: boolean;
    /** 时间格式 */
    timeFormat?: string;
    /** 主题色 */
    tone?: Tone;
    /** 数值单位（悬浮提示用） */
    unit?: string;
  }>(),
  {
    area: true,
    bars: false,
    height: 190,
    markers: true,
    timeFormat: 'HH:mm',
    tone: 'blue',
    unit: '',
  },
);

const uid = useId();

const wrapRef = ref<HTMLElement>();
const { width } = useElementSize(wrapRef);
const hoverIndex = ref<null | number>(null);

const PAD = { bottom: 24, left: 42, right: 12, top: 14 };

const chartWidth = computed(() => Math.max(0, width.value));
const innerWidth = computed(() =>
  Math.max(0, chartWidth.value - PAD.left - PAD.right),
);
const innerHeight = computed(() => Math.max(0, props.height - PAD.top - PAD.bottom));

const values = computed(() => props.data.map((d) => d.value));
const maxValue = computed(() => Math.max(0, ...values.value));
const avgValue = computed(() => {
  if (values.value.length === 0) return 0;
  return values.value.reduce((sum, v) => sum + v, 0) / values.value.length;
});

function niceCeil(v: number): number {
  if (v <= 0) return 1;
  const exp = Math.floor(Math.log10(v));
  const base = 10 ** exp;
  const f = v / base;
  const steps = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10];
  const step = steps.find((s) => f <= s) ?? 10;
  return step * base;
}

const axisMax = computed(() => niceCeil(maxValue.value * 1.12 || 1));

const stepX = computed(() => {
  const n = props.data.length;
  if (n <= 1) return 0;
  return props.bars ? innerWidth.value / n : innerWidth.value / (n - 1);
});

function xAt(i: number): number {
  if (props.bars) {
    const n = props.data.length || 1;
    return PAD.left + ((i + 0.5) * innerWidth.value) / n;
  }
  return PAD.left + i * stepX.value;
}

function yAt(v: number): number {
  return PAD.top + innerHeight.value * (1 - v / axisMax.value);
}

const points = computed(() =>
  props.data.map((d, i) => ({ x: xAt(i), y: yAt(d.value) })),
);

/** 单调三次插值：平滑且不过冲，避免折线出现「假峰值」 */
const linePath = computed(() => {
  const pts = points.value;
  const n = pts.length;
  const first = pts[0];
  const last = pts[n - 1];
  if (!first || !last) return '';
  if (n === 1) return `M ${first.x} ${first.y}`;

  const dxs: number[] = [];
  const slopes: number[] = [];
  for (let i = 0; i < n - 1; i += 1) {
    const a = pts[i] ?? first;
    const b = pts[i + 1] ?? last;
    const dx = b.x - a.x;
    dxs.push(dx);
    slopes.push((b.y - a.y) / (dx || 1));
  }

  const tangent: number[] = [slopes[0] ?? 0];
  for (let i = 1; i < n - 1; i += 1) {
    const m0 = slopes[i - 1] ?? 0;
    const m1 = slopes[i] ?? 0;
    const prev = dxs[i - 1] ?? 1;
    const next = dxs[i] ?? 1;
    if (m0 * m1 <= 0) {
      tangent.push(0);
    } else {
      const w1 = 2 * next + prev;
      const w2 = next + 2 * prev;
      tangent.push((w1 + w2) / (w1 / m0 + w2 / m1));
    }
  }
  tangent.push(slopes[n - 2] ?? 0);

  let d = `M ${first.x} ${first.y}`;
  for (let i = 0; i < n - 1; i += 1) {
    const a = pts[i] ?? first;
    const b = pts[i + 1] ?? last;
    const dx = dxs[i] ?? 1;
    const t0 = tangent[i] ?? 0;
    const t1 = tangent[i + 1] ?? 0;
    const x1 = a.x + dx / 3;
    const y1 = a.y + (t0 * dx) / 3;
    const x2 = b.x - dx / 3;
    const y2 = b.y - (t1 * dx) / 3;
    d += ` C ${x1.toFixed(1)} ${y1.toFixed(1)}, ${x2.toFixed(1)} ${y2.toFixed(1)}, ${b.x.toFixed(1)} ${b.y.toFixed(1)}`;
  }
  return d;
});

const areaPath = computed(() => {
  const first = points.value[0];
  const last = points.value.at(-1);
  if (!first || !last) return '';
  const baseline = PAD.top + innerHeight.value;
  return `${linePath.value} L ${last.x.toFixed(1)} ${baseline} L ${first.x.toFixed(1)} ${baseline} Z`;
});

const lastPoint = computed(() => points.value.at(-1) ?? null);
const hoverPoint = computed(() =>
  hoverIndex.value === null ? null : points.value[hoverIndex.value] ?? null,
);

const barWidth = computed(() => {
  const n = props.data.length || 1;
  return Math.max(2, Math.min(26, (innerWidth.value / n) * 0.56));
});

const yTicks = computed(() =>
  [0, 0.5, 1].map((r) => ({ value: axisMax.value * r, y: yAt(axisMax.value * r) })),
);

const xTicks = computed(() => {
  const n = props.data.length;
  if (n === 0) return [];
  const idx = new Set<number>([
    0,
    Math.round(((n - 1) * 2) / 3),
    Math.round((n - 1) / 3),
    n - 1,
  ]);
  return [...idx]
    .filter((i) => i >= 0 && i < n)
    .map((i) => {
      const point = props.data[i];
      return {
        i,
        x: xAt(i),
        label: point ? dayjs(point.ts).format(props.timeFormat) : '',
      };
    });
});

const avgY = computed(() => yAt(avgValue.value));
const peakY = computed(() => yAt(maxValue.value));

function fmtAxis(v: number): string {
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(v % 1_000_000 === 0 ? 0 : 1)}M`;
  if (v >= 10_000) return `${(v / 1000).toFixed(0)}k`;
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`;
  return String(Math.round(v));
}

function onMove(e: MouseEvent) {
  const n = props.data.length;
  if (n === 0) return;
  const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
  const x = e.clientX - rect.left;
  if (props.bars) {
    const step = innerWidth.value / n;
    hoverIndex.value = Math.min(n - 1, Math.max(0, Math.floor((x - PAD.left) / step)));
    return;
  }
  const ratio = stepX.value === 0 ? 0 : (x - PAD.left) / stepX.value;
  hoverIndex.value = Math.min(n - 1, Math.max(0, Math.round(ratio)));
}

const tooltip = computed(() => {
  const i = hoverIndex.value;
  if (i === null) return null;
  const point = points.value[i];
  const d = props.data[i];
  if (!point || !d) return null;
  const tipWidth = 132;
  const left = Math.min(
    Math.max(point.x, tipWidth / 2 + 4),
    Math.max(tipWidth / 2 + 4, chartWidth.value - tipWidth / 2 - 4),
  );
  return {
    left,
    top: Math.max(48, point.y - 12),
    time: dayjs(d.ts).format('YYYY-MM-DD HH:mm'),
    value: d.value,
  };
});

const toneVar = computed(() => `hsl(var(--ipam-tone-${props.tone}))`);
const lineStyle = computed<CSSProperties>(() => ({
  stroke: toneVar.value,
}));
</script>

<template>
  <div ref="wrapRef" class="ipam-chart" :style="{ height: `${height}px` }">
    <svg
      v-if="chartWidth > 0 && data.length > 0"
      class="ipam-chart__svg"
      :height="height"
      :viewBox="`0 0 ${chartWidth} ${height}`"
      :width="chartWidth"
      role="img"
      @mouseleave="hoverIndex = null"
      @mousemove="onMove"
    >
      <defs>
        <linearGradient :id="`${uid}-fill`" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" :stop-color="toneVar" stop-opacity="0.26" />
          <stop offset="62%" :stop-color="toneVar" stop-opacity="0.08" />
          <stop offset="100%" :stop-color="toneVar" stop-opacity="0" />
        </linearGradient>
        <linearGradient :id="`${uid}-bar`" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" :stop-color="toneVar" stop-opacity="0.95" />
          <stop offset="100%" :stop-color="toneVar" stop-opacity="0.35" />
        </linearGradient>
      </defs>

      <!-- 网格与 Y 轴刻度 -->
      <g>
        <line
          v-for="t in yTicks"
          :key="`g-${t.y}`"
          :x1="PAD.left"
          :x2="chartWidth - PAD.right"
          :y1="t.y"
          :y2="t.y"
          stroke="hsl(var(--border) / 70%)"
          stroke-dasharray="3 5"
          stroke-width="1"
        />
        <text
          v-for="t in yTicks"
          :key="`y-${t.y}`"
          :x="PAD.left - 8"
          :y="t.y + 3.5"
          fill="hsl(var(--muted-foreground))"
          font-size="10"
          text-anchor="end"
        >
          {{ fmtAxis(t.value) }}
        </text>
      </g>

      <!-- 均值 / 峰值参考线 -->
      <g v-if="markers && data.length > 1">
        <line
          :stroke="toneVar"
          stroke-dasharray="4 4"
          stroke-opacity="0.45"
          stroke-width="1"
          :x1="PAD.left"
          :x2="chartWidth - PAD.right"
          :y1="peakY"
          :y2="peakY"
        />
        <line
          stroke="hsl(var(--muted-foreground) / 45%)"
          stroke-dasharray="2 4"
          stroke-width="1"
          :x1="PAD.left"
          :x2="chartWidth - PAD.right"
          :y1="avgY"
          :y2="avgY"
        />
      </g>

      <!-- 柱状 -->
      <g v-if="bars">
        <rect
          v-for="(p, i) in points"
          :key="`b-${i}`"
          :fill="`url(#${uid}-bar)`"
          :height="Math.max(2, PAD.top + innerHeight - p.y)"
          :opacity="hoverIndex === null || hoverIndex === i ? 1 : 0.6"
          rx="3"
          :width="barWidth"
          :x="p.x - barWidth / 2"
          :y="p.y"
        />
      </g>

      <!-- 折线 + 面积 -->
      <g v-else>
        <path v-if="area" :d="areaPath" :fill="`url(#${uid}-fill)`" />
        <path
          :d="linePath"
          fill="none"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          :style="lineStyle"
        />
        <!-- 末端脉冲点 -->
        <template v-if="lastPoint">
          <circle
            :cx="lastPoint.x"
            :cy="lastPoint.y"
            :fill="toneVar"
            r="3.5"
          />
          <circle
            class="ipam-chart__pulse"
            :cx="lastPoint.x"
            :cy="lastPoint.y"
            :fill="toneVar"
            r="3.5"
          />
        </template>
      </g>

      <!-- 悬浮指示 -->
      <g v-if="hoverPoint">
        <line
          :x1="hoverPoint.x"
          :x2="hoverPoint.x"
          :y1="PAD.top"
          :y2="PAD.top + innerHeight"
          stroke="hsl(var(--foreground) / 22%)"
          stroke-dasharray="3 3"
          stroke-width="1"
        />
        <circle
          :cx="hoverPoint.x"
          :cy="hoverPoint.y"
          fill="hsl(var(--card))"
          :r="5.5"
          :stroke="toneVar"
          stroke-width="2.5"
        />
      </g>

      <!-- X 轴刻度 -->
      <text
        v-for="t in xTicks"
        :key="`x-${t.i}`"
        fill="hsl(var(--muted-foreground))"
        font-size="10"
        :text-anchor="t.i === 0 ? 'start' : t.i === data.length - 1 ? 'end' : 'middle'"
        :x="t.x"
        :y="height - 6"
      >
        {{ t.label }}
      </text>
    </svg>

    <div
      v-if="tooltip"
      class="ipam-chart__tip"
      :style="{ left: `${tooltip.left}px`, top: `${tooltip.top}px` }"
    >
      <div class="ipam-chart__tip-time">{{ tooltip.time }}</div>
      <div class="ipam-chart__tip-value">
        <i :style="{ background: toneVar }"></i>
        <span>{{ tooltip.value.toLocaleString() }}</span>
        <small v-if="unit">{{ unit }}</small>
      </div>
    </div>
  </div>
</template>
