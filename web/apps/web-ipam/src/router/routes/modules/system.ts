import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:settings',
      order: 5,
      title: $t('page.system.title'),
    },
    name: 'System',
    path: '/system',
    children: [
      {
        name: 'SystemOrgs',
        path: '/system/orgs',
        component: () => import('#/views/system/orgs/index.vue'),
        meta: { icon: 'lucide:git-branch', title: $t('page.system.orgs'), authority: ['system:read'] },
      },
      {
        name: 'SystemUsers',
        path: '/system/users',
        component: () => import('#/views/system/users/index.vue'),
        meta: { icon: 'lucide:users', title: $t('page.system.users'), authority: ['system:read'] },
      },
      {
        name: 'SystemRoles',
        path: '/system/roles',
        component: () => import('#/views/system/roles/index.vue'),
        meta: { icon: 'lucide:shield-check', title: $t('page.system.roles'), authority: ['system:read'] },
      },
      {
        name: 'SystemSecurity',
        path: '/system/security',
        component: () => import('#/views/system/security/index.vue'),
        meta: { icon: 'lucide:key-round', title: $t('page.system.security'), authority: ['system:read'] },
      },
      {
        name: 'SystemBranding',
        path: '/system/branding',
        component: () => import('#/views/system/branding/index.vue'),
        meta: { icon: 'lucide:palette', title: $t('page.system.branding'), authority: ['system:read'] },
      },
      {
        name: 'SystemRuntime',
        path: '/system/runtime',
        component: () => import('#/views/system/runtime/index.vue'),
        meta: { icon: 'lucide:server-cog', title: $t('page.system.runtime'), authority: ['system:read'] },
      },
      {
        name: 'SystemAbout',
        path: '/system/about',
        component: () => import('#/views/_core/about/index.vue'),
        meta: { icon: 'lucide:copyright', title: $t('page.system.about'), authority: ['system:read'] },
      },
      // 兼容旧书签：原“系统设置”已拆分为组织/品牌/账号安全/服务与运行
      {
        name: 'SystemSettingsLegacy',
        path: '/system/settings',
        redirect: '/system/orgs',
        meta: { hideInMenu: true, title: $t('page.system.settings') },
      },
    ],
  },
];

export default routes;
