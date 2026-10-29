<script setup lang="ts">
import { IconifyIcon } from '@vben/icons';

withDefaults(
  defineProps<{
    /** 一句话说明页面用途，建议 20-40 字 */
    description?: string;
    /** 标题上方的模块标记（可选） */
    eyebrow?: string;
    /** lucide 图标名，如 lucide:network */
    icon?: string;
    /** 页头主标题 */
    title: string;
  }>(),
  {
    description: '',
    eyebrow: '',
    icon: 'lucide:layout-dashboard',
  },
);
</script>

<template>
  <header class="ipam-page-header">
    <div class="ipam-page-header__main">
      <div class="ipam-page-header__icon">
        <IconifyIcon :icon="icon" class="size-5" />
      </div>
      <div class="min-w-0">
        <div v-if="eyebrow" class="ipam-page-header__eyebrow">
          {{ eyebrow }}
        </div>
        <h1 class="ipam-page-header__title">
          <slot name="title">{{ title }}</slot>
        </h1>
        <p v-if="description || $slots.description" class="ipam-page-header__desc">
          <slot name="description">{{ description }}</slot>
        </p>
        <slot name="meta"></slot>
      </div>
    </div>
    <div v-if="$slots.actions || $slots.default" class="ipam-page-header__actions">
      <slot name="actions"></slot>
      <slot></slot>
    </div>
  </header>
</template>
