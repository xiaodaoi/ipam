<script setup lang="ts">
import type { CSSProperties } from 'vue';

import { computed } from 'vue';

type Tone = 'amber' | 'blue' | 'cyan' | 'green' | 'red' | 'violet';

const props = withDefaults(
  defineProps<{
    /** 中心说明 */
    caption?: string;
    /** 是否无数据（显示占位） */
    empty?: boolean;
    /** 中心主数值（缺省用 value 拼百分号） */
    label?: string;
    /** 环形尺寸（px） */
    size?: number;
    /** 环宽（px） */
    thickness?: number;
    /** 主题色 */
    tone?: Tone;
    /** 0-100 */
    value?: number;
  }>(),
  {
    caption: '',
    empty: false,
    label: '',
    size: 118,
    thickness: 9,
    tone: 'blue',
    value: 0,
  },
);

const radius = computed(() => (props.size - props.thickness) / 2);
const circumference = computed(() => 2 * Math.PI * radius.value);
const pct = computed(() => Math.max(0, Math.min(100, props.value)));
const dash = computed(() => `${(pct.value / 100) * circumference.value} ${circumference.value}`);
const accent = computed(() => `hsl(var(--ipam-tone-${props.tone}))`);
const centerText = computed(() =>
  props.label === '' ? `${Math.round(pct.value)}%` : props.label,
);
const ringStyle = computed<CSSProperties>(() => ({
  stroke: props.empty ? 'hsl(var(--muted-foreground) / 35%)' : accent.value,
}));
</script>

<template>
  <div class="ipam-ring" :style="{ width: `${size}px` }">
    <div class="ipam-ring__chart" :style="{ height: `${size}px`, width: `${size}px` }">
      <svg :height="size" :viewBox="`0 0 ${size} ${size}`" :width="size">
        <circle
          fill="none"
          :cx="size / 2"
          :cy="size / 2"
          :r="radius"
          stroke="hsl(var(--muted))"
          :stroke-width="thickness"
        />
        <circle
          v-if="!empty"
          class="ipam-ring__value"
          fill="none"
          stroke-linecap="round"
          :stroke-dasharray="dash"
          :stroke-width="thickness"
          :style="ringStyle"
          :transform="`rotate(-90 ${size / 2} ${size / 2})`"
          :cx="size / 2"
          :cy="size / 2"
          :r="radius"
        />
      </svg>
      <div class="ipam-ring__center">
        <div class="ipam-ring__value-text" :data-empty="empty">
          {{ empty ? '—' : centerText }}
        </div>
        <div v-if="caption" class="ipam-ring__caption">{{ caption }}</div>
      </div>
    </div>
    <slot></slot>
  </div>
</template>
