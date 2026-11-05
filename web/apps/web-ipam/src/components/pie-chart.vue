<script setup lang="ts">
import { computed } from 'vue';

type Tone = 'amber' | 'blue' | 'cyan' | 'green' | 'red' | 'violet';

interface PieItem {
  label: string;
  tone?: Tone;
  value: number;
}

const props = withDefaults(
  defineProps<{
    /** 中心说明（仅在环形模式显示） */
    centerCaption?: string;
    /** 中心汇总值（字符串，便于调用方自行格式化） */
    centerValue?: string;
    /** 无数据占位文案 */
    emptyText?: string;
    /** 图例/悬浮数值格式化 */
    formatter?: (v: number) => string;
    /** 扇区数据，value<=0 的项自动忽略 */
    items: PieItem[];
    /** 画布尺寸（px） */
    size?: number;
    /** 环宽（px）；0 为实心饼图 */
    thickness?: number;
  }>(),
  {
    centerCaption: '',
    centerValue: '',
    emptyText: '—',
    formatter: undefined,
    size: 168,
    thickness: 26,
  },
);

const TONES: Tone[] = ['blue', 'cyan', 'green', 'amber', 'violet', 'red'];

const positive = computed(() =>
  props.items.filter((i) => Number.isFinite(i.value) && i.value > 0),
);
const total = computed(() =>
  positive.value.reduce((sum, i) => sum + i.value, 0),
);
const slices = computed(() => {
  const list = positive.value;
  const sum = total.value;
  if (list.length === 0 || sum <= 0) return [];
  let cursor = 0;
  return list.map((item, index) => {
    const frac = item.value / sum;
    const start = cursor * 360;
    const end = (cursor + frac) * 360;
    cursor += frac;
    const tone = item.tone ?? TONES[index % TONES.length] ?? 'blue';
    return {
      color: `hsl(var(--ipam-tone-${tone}))`,
      end,
      key: `${index}-${item.label}`,
      label: item.label,
      pct: frac * 100,
      start,
      value: item.value,
    };
  });
});

const radius = computed(() => props.size / 2 - 2);
const innerRadius = computed(() =>
  Math.max(0, radius.value - Math.max(0, props.thickness)),
);
const center = computed(() => props.size / 2);

function polar(r: number, deg: number) {
  const rad = ((deg - 90) * Math.PI) / 180;
  return {
    x: center.value + r * Math.cos(rad),
    y: center.value + r * Math.sin(rad),
  };
}

/** 生成扇区路径；单扇区（100%）时留 0.01° 避免起止点重合导致不渲染。 */
function pathFor(start: number, end: number): string {
  const sweep = Math.min(end, start + 359.99);
  const large = sweep - start > 180 ? 1 : 0;
  const outerStart = polar(radius.value, start);
  const outerEnd = polar(radius.value, sweep);
  if (innerRadius.value <= 0) {
    return [
      `M ${center.value} ${center.value}`,
      `L ${outerStart.x} ${outerStart.y}`,
      `A ${radius.value} ${radius.value} 0 ${large} 1 ${outerEnd.x} ${outerEnd.y}`,
      'Z',
    ].join(' ');
  }
  const innerStart = polar(innerRadius.value, start);
  const innerEnd = polar(innerRadius.value, sweep);
  return [
    `M ${outerStart.x} ${outerStart.y}`,
    `A ${radius.value} ${radius.value} 0 ${large} 1 ${outerEnd.x} ${outerEnd.y}`,
    `L ${innerEnd.x} ${innerEnd.y}`,
    `A ${innerRadius.value} ${innerRadius.value} 0 ${large} 0 ${innerStart.x} ${innerStart.y}`,
    'Z',
  ].join(' ');
}

const format = computed(() => props.formatter ?? String);
const showCenter = computed(
  () => Boolean(props.centerValue) || Boolean(props.centerCaption),
);
const ariaLabel = computed(() =>
  slices.value.map((s) => `${s.label} ${s.pct.toFixed(1)}%`).join(', '),
);
</script>

<template>
  <div class="ipam-pie">
    <div
      v-if="slices.length > 0"
      class="ipam-pie__chart"
      :style="{ height: `${size}px`, width: `${size}px` }"
    >
      <svg
        :aria-label="ariaLabel"
        :height="size"
        role="img"
        :viewBox="`0 0 ${size} ${size}`"
        :width="size"
      >
        <path
          v-for="slice in slices"
          :key="slice.key"
          class="ipam-pie__slice"
          :d="pathFor(slice.start, slice.end)"
          :fill="slice.color"
          stroke="hsl(var(--card))"
          stroke-width="1"
        >
          <title>
            {{ slice.label }} {{ format(slice.value) }}（{{
              slice.pct.toFixed(1)
            }}%）
          </title>
        </path>
      </svg>
      <div v-if="showCenter" class="ipam-pie__center">
        <span v-if="centerValue" class="ipam-pie__value">{{
          centerValue
        }}</span>
        <span v-if="centerCaption" class="ipam-pie__caption">{{
          centerCaption
        }}</span>
      </div>
    </div>
    <div
      v-else
      class="ipam-pie__empty"
      :style="{ height: `${size}px`, width: `${size}px` }"
    >
      {{ emptyText }}
    </div>

    <ul v-if="slices.length > 0" class="ipam-pie__legend">
      <li v-for="slice in slices" :key="`legend-${slice.key}`">
        <i :style="{ background: slice.color }"></i>
        <span class="ipam-pie__label" :title="slice.label">{{
          slice.label
        }}</span>
        <span class="ipam-pie__legend-value">{{ format(slice.value) }}</span>
        <span class="ipam-pie__legend-pct">{{ slice.pct.toFixed(1) }}%</span>
      </li>
    </ul>
  </div>
</template>
