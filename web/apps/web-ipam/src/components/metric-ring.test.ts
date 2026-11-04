import { mount } from '@vue/test-utils';

import { describe, expect, it } from 'vitest';

import MetricRing from './metric-ring.vue';

describe('metric-ring 指标环', () => {
  it('默认以四舍五入百分比作为中心文本', () => {
    const wrapper = mount(MetricRing, { props: { value: 42.6 } });
    expect(wrapper.find('.ipam-ring__value-text').text()).toBe('43%');
  });

  it('数值越界时钳制到 0–100', async () => {
    const wrapper = mount(MetricRing, { props: { value: -20 } });
    expect(wrapper.find('.ipam-ring__value-text').text()).toBe('0%');
    await wrapper.setProps({ value: 180 });
    expect(wrapper.find('.ipam-ring__value-text').text()).toBe('100%');
  });

  it('label 覆盖中心文本，caption 作为说明渲染', () => {
    const wrapper = mount(MetricRing, {
      props: { caption: '已用容量', label: '3.2 TB', value: 10 },
    });
    expect(wrapper.find('.ipam-ring__value-text').text()).toBe('3.2 TB');
    expect(wrapper.find('.ipam-ring__caption').text()).toBe('已用容量');
  });

  it('empty 显示占位且不渲染数值圆环', () => {
    const wrapper = mount(MetricRing, { props: { empty: true, value: 80 } });
    expect(wrapper.find('.ipam-ring__value-text').text()).toBe('—');
    expect(
      wrapper.find('.ipam-ring__value-text').attributes('data-empty'),
    ).toBe('true');
    expect(wrapper.find('.ipam-ring__value').exists()).toBe(false);
  });

  it('tone 映射到主题色变量', () => {
    const wrapper = mount(MetricRing, { props: { tone: 'red', value: 95 } });
    const ring = wrapper.find('.ipam-ring__value');
    expect(ring.attributes('style')).toContain('hsl(var(--ipam-tone-red))');
  });
});
