import { mount } from '@vue/test-utils';

import { describe, expect, it } from 'vitest';

import PieChart from './pie-chart.vue';

describe('pie-chart 饼图', () => {
  it('无有效数据时不渲染 SVG，显示占位', () => {
    const wrapper = mount(PieChart, {
      props: {
        emptyText: '暂无数据',
        items: [
          { label: '空', value: 0 },
          { label: '负数', value: -1 },
        ],
      },
    });
    expect(wrapper.find('svg').exists()).toBe(false);
    expect(wrapper.text()).toContain('暂无数据');
  });

  it('按占比绘制扇区并输出图例', () => {
    const wrapper = mount(PieChart, {
      props: {
        items: [
          { label: '已用', value: 75 },
          { label: '可用', value: 25 },
        ],
      },
    });
    expect(wrapper.findAll('path')).toHaveLength(2);
    const legend = wrapper.findAll('.ipam-pie__legend li');
    expect(legend).toHaveLength(2);
    expect(legend[0]?.text()).toContain('已用');
    expect(legend[0]?.text()).toContain('75.0%');
    expect(legend[1]?.text()).toContain('25.0%');
  });

  it('单扇区（100%）也能渲染路径', () => {
    const wrapper = mount(PieChart, {
      props: { items: [{ label: '全部', value: 10 }] },
    });
    const paths = wrapper.findAll('path');
    expect(paths).toHaveLength(1);
    expect(paths[0]?.attributes('d')).toContain('A');
    expect(wrapper.find('.ipam-pie__legend-pct').text()).toBe('100.0%');
  });

  it('formatter 同时作用于图例与悬浮提示', () => {
    const wrapper = mount(PieChart, {
      props: {
        formatter: (v: number) => `${v} GB`,
        items: [{ label: '根分区', value: 12 }],
      },
    });
    expect(wrapper.find('.ipam-pie__legend-value').text()).toBe('12 GB');
    expect(wrapper.find('title').text()).toContain('12 GB');
  });

  it('传 centerValue 时显示中心汇总值', () => {
    const wrapper = mount(PieChart, {
      props: {
        centerCaption: '已用',
        centerValue: '1.5 TB',
        items: [{ label: '根分区', value: 1 }],
      },
    });
    expect(wrapper.find('.ipam-pie__value').text()).toBe('1.5 TB');
    expect(wrapper.find('.ipam-pie__caption').text()).toBe('已用');
  });
});
