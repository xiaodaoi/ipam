import { mount } from '@vue/test-utils';

import { describe, expect, it, vi } from 'vitest';

import PageHeader from './page-header.vue';

// 图标组件会触发 iconify 远程取图（happy-dom 下会挂起 fetch），统一替身。
vi.mock('@vben/icons', () => ({
  IconifyIcon: { name: 'IconifyIcon', template: '<i class="stub-icon" />' },
}));

describe('page-header 页头', () => {
  it('渲染标题/说明/模块标记', () => {
    const wrapper = mount(PageHeader, {
      props: { description: '统一查看子网与地址池', eyebrow: 'IPAM', title: '子网管理' },
    });
    expect(wrapper.find('.ipam-page-header__title').text()).toBe('子网管理');
    expect(wrapper.find('.ipam-page-header__desc').text()).toBe('统一查看子网与地址池');
    expect(wrapper.find('.ipam-page-header__eyebrow').text()).toBe('IPAM');
  });

  it('缺省说明时不渲染 desc 节点', () => {
    const wrapper = mount(PageHeader, { props: { title: '仪表盘' } });
    expect(wrapper.find('.ipam-page-header__desc').exists()).toBe(false);
  });

  it('title/description 具名插槽优先于 props', () => {
    const wrapper = mount(PageHeader, {
      props: { description: 'props 说明', title: 'props 标题' },
      slots: { description: '插槽说明', title: '<b>插槽标题</b>' },
    });
    expect(wrapper.find('.ipam-page-header__title').text()).toBe('插槽标题');
    expect(wrapper.find('.ipam-page-header__desc').text()).toBe('插槽说明');
  });

  it('actions/default 插槽进入右侧操作区', () => {
    const wrapper = mount(PageHeader, {
      props: { title: '日志' },
      slots: { actions: '<button class="act">导出</button>', default: '<span class="extra">更多</span>' },
    });
    const actions = wrapper.find('.ipam-page-header__actions');
    expect(actions.find('.act').exists()).toBe(true);
    expect(actions.find('.extra').exists()).toBe(true);
  });
});
