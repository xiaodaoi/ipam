import { mount } from '@vue/test-utils';

import { describe, expect, it, vi } from 'vitest';

import TrendChart from './trend-chart.vue';

// happy-dom 无布局：固定容器宽度，让坐标/路径可断言。
vi.mock('@vueuse/core', async () => {
  const { ref } = await import('vue');
  return { useElementSize: () => ({ width: ref(600) }) };
});

const data = [
  { ts: '2026-11-01T10:00:00Z', value: 10 },
  { ts: '2026-11-01T10:05:00Z', value: 30 },
  { ts: '2026-11-01T10:10:00Z', value: 20 },
];

describe('trend-chart 趋势图', () => {
  it('无数据时不渲染 SVG', () => {
    const wrapper = mount(TrendChart, { props: { data: [] } });
    expect(wrapper.find('svg').exists()).toBe(false);
  });

  it('折线模式绘制平滑曲线与面积', () => {
    const wrapper = mount(TrendChart, { props: { area: true, data } });
    const svg = wrapper.find('svg');
    expect(svg.exists()).toBe(true);
    const paths = svg.findAll('path');
    expect(paths.length).toBe(2); // 面积 + 折线
    expect(paths[1]?.attributes('d')).toContain('C');
  });

  it('X 轴刻度按 timeFormat 格式化', () => {
    const wrapper = mount(TrendChart, {
      props: { data, timeFormat: 'HH:mm' },
    });
    const labels = wrapper.findAll('text').map((t) => t.text());
    expect(labels).toContain('10:00');
    expect(labels).toContain('10:10');
  });

  it('柱状模式渲染每点一根柱且不绘制面积', () => {
    const wrapper = mount(TrendChart, { props: { bars: true, data } });
    expect(wrapper.findAll('rect').length).toBe(3);
    expect(wrapper.findAll('path').length).toBe(0);
  });

  it('markers 开关控制均值/峰值参考线', async () => {
    const wrapper = mount(TrendChart, { props: { data, markers: true } });
    const dashed = () =>
      wrapper
        .findAll('line')
        .filter((l) => ['2 4', '4 4'].includes(l.attributes('stroke-dasharray') ?? '')).length;
    expect(dashed()).toBe(2);
    await wrapper.setProps({ markers: false });
    expect(dashed()).toBe(0);
  });

  it('悬浮时展示时间与数值提示', async () => {
    const wrapper = mount(TrendChart, { props: { data, unit: 'QPS' } });
    await wrapper.find('svg').trigger('mousemove', { clientX: 315 });
    const tip = wrapper.find('.ipam-chart__tip');
    expect(tip.exists()).toBe(true);
    expect(tip.text()).toContain('2026-11-01 10:05');
    expect(tip.text()).toContain('30');
    expect(tip.text()).toContain('QPS');
    await wrapper.find('svg').trigger('mouseleave');
    expect(wrapper.find('.ipam-chart__tip').exists()).toBe(false);
  });
});
