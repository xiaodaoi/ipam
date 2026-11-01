import { mount } from '@vue/test-utils';

import { describe, expect, it, vi } from 'vitest';

import StatCard from './stat-card.vue';

// 图标组件会触发 iconify 远程取图（happy-dom 下会挂起 fetch），统一替身。
vi.mock('@vben/icons', () => ({
  IconifyIcon: { name: 'IconifyIcon', template: '<i class="stub-icon" />' },
}));

describe('stat-card 指标卡', () => {
  it('渲染名称、数值与单位', () => {
    const wrapper = mount(StatCard, {
      props: { label: '在线终端', unit: '台', value: 128 },
    });
    expect(wrapper.find('.ipam-stat-card__label').text()).toBe('在线终端');
    expect(wrapper.find('.ipam-stat-card__value').text()).toContain('128');
    expect(wrapper.find('.ipam-stat-card__value small').text()).toBe('台');
  });

  it('趋势文案携带方向语义', () => {
    const wrapper = mount(StatCard, {
      props: { label: '新增', trend: '+12.4%', trendTone: 'up', value: 12 },
    });
    const trend = wrapper.find('.ipam-stat-card__trend');
    expect(trend.text()).toBe('+12.4%');
    expect(trend.attributes('data-tone')).toBe('up');
  });

  it('进度条宽度钳制在 0–100%', async () => {
    const wrapper = mount(StatCard, { props: { label: '水位', progress: 150, value: 1 } });
    expect(wrapper.find('.ipam-stat-card__progress i').attributes('style')).toContain('width: 100%');
    await wrapper.setProps({ progress: -5 });
    expect(wrapper.find('.ipam-stat-card__progress i').attributes('style')).toContain('width: 0%');
  });

  it('未传 progress 时不渲染进度条', () => {
    const wrapper = mount(StatCard, { props: { label: '水位', value: 1 } });
    expect(wrapper.find('.ipam-stat-card__progress').exists()).toBe(false);
  });

  it('spark 少于两个点时不渲染迷你趋势图', () => {
    const wrapper = mount(StatCard, { props: { label: 'QPS', spark: [3], value: 3 } });
    expect(wrapper.find('.ipam-stat-card__spark').exists()).toBe(false);
  });

  it('spark 数据生成折线点，渐变 id 稳定', async () => {
    const wrapper = mount(StatCard, {
      props: { label: 'QPS', spark: [1, 3, 2], tone: 'green', value: 3 },
    });
    const polyline = wrapper.find('.ipam-stat-card__spark polyline');
    expect(polyline.attributes('points')?.split(' ').length).toBe(3);
    const firstId = wrapper.find('.ipam-stat-card__spark linearGradient').attributes('id');
    await wrapper.setProps({ tone: 'red' });
    const secondId = wrapper.find('.ipam-stat-card__spark linearGradient').attributes('id');
    expect(secondId).toBe(firstId);
    expect(wrapper.find('.ipam-stat-card').attributes('data-tone')).toBe('red');
  });

  it('hint 插槽优先于 props', () => {
    const wrapper = mount(StatCard, {
      props: { hint: 'props 提示', label: 'QPS', value: 1 },
      slots: { hint: '插槽提示' },
    });
    expect(wrapper.find('.ipam-stat-card__hint').text()).toBe('插槽提示');
  });
});
