<script setup lang="ts">
import type { CSSProperties } from 'vue';

import { computed } from 'vue';

import { IconifyIcon } from '@vben/icons';

type Tone = 'amber' | 'blue' | 'cyan' | 'green' | 'red' | 'violet';

const props = withDefaults(
  defineProps<{
    /** 底部说明 */
    hint?: string;
    /** lucide 图标名 */
    icon?: string;
    /** 指标名称 */
    label: string;
    /** 0-100 的进度，用于底部进度条 */
    progress?: number;
    /** 迷你趋势数据（仅示意形状，不带坐标轴） */
    spark?: number[];
    /** 迷你趋势色调，缺省跟随 tone */
    sparkTone?: Tone;
    /** 主题色 */
    tone?: Tone;
    /** 趋势文案，如 “+12.4%” */
    trend?: string;
    /** 趋势方向 */
    trendTone?: 'down' | 'flat' | 'up';
    /** 数值单位 */
    unit?: string;
    /** 主数值 */
    value: number | string;
  }>(),
  {
    hint: '',
    icon: 'lucide:activity',
    progress: undefined,
    spark: undefined,
    sparkTone: undefined,
    tone: 'blue',
    trend: '',
    trendTone: 'flat',
    unit: '',
  },
);

const progressWidth = computed(() => {
  if (props.progress === undefined || props.progress === null) return '';
  return `${Math.max(0, Math.min(100, props.progress))}%`;
});

let seed = 0;
const uid = computed(() => `ipam-spark-${(seed += 1)}-${props.tone}`);

const sparkPoints = computed(() => {
  const list = props.spark ?? [];
  if (list.length < 2) return null;
  const max = Math.max(...list, 1);
  const min = Math.min(...list);
  const span = Math.max(1, max - min);
  const w = 100;
  const h = 26;
  const step = w / (list.length - 1);
  return list.map((v, i) => ({
    x: i * step,
    y: h - 2 - ((v - min) / span) * (h - 4),
  }));
});

const sparkLine = computed(() =>
  (sparkPoints.value ?? []).map((p) => `${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' '),
);
const sparkArea = computed(() =>
  sparkPoints.value ? `0,28 ${sparkLine.value} 100,28` : '',
);
const sparkStyle = computed<CSSProperties>(() => ({
  color: `hsl(var(--ipam-tone-${props.sparkTone ?? props.tone}))`,
}));
</script>

<template>
  <div class="ipam-stat-card" :data-tone="tone">
    <div class="ipam-stat-card__top">
      <span class="ipam-stat-card__label">{{ label }}</span>
      <span class="ipam-stat-card__icon">
        <IconifyIcon :icon="icon" class="size-[18px]" />
      </span>
    </div>
    <div class="ipam-stat-card__value">
      <span>{{ value }}</span>
      <small v-if="unit">{{ unit }}</small>
      <span
        v-if="trend"
        class="ipam-stat-card__trend ipam-stat-card__trend--value"
        :data-tone="trendTone"
      >
        <IconifyIcon
          v-if="trendTone === 'up'"
          icon="lucide:trending-up"
          class="size-3.5"
        />
        <IconifyIcon
          v-else-if="trendTone === 'down'"
          icon="lucide:trending-down"
          class="size-3.5"
        />
        {{ trend }}
      </span>
    </div>
    <div v-if="sparkPoints" class="ipam-stat-card__spark" :style="sparkStyle">
      <svg preserveAspectRatio="none" viewBox="0 0 100 28">
        <defs>
          <linearGradient :id="uid" x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stop-color="currentColor" stop-opacity="0.24" />
            <stop offset="100%" stop-color="currentColor" stop-opacity="0" />
          </linearGradient>
        </defs>
        <polygon :fill="`url(#${uid})`" :points="sparkArea" />
        <polyline
          fill="none"
          :points="sparkLine"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.6"
          vector-effect="non-scaling-stroke"
        />
      </svg>
    </div>
    <div class="ipam-stat-card__bottom">
      <span class="ipam-stat-card__hint">
        <slot name="hint">{{ hint }}</slot>
      </span>
    </div>
    <div v-if="progress !== undefined" class="ipam-stat-card__progress">
      <i :style="{ width: progressWidth }"></i>
    </div>
  </div>
</template>
