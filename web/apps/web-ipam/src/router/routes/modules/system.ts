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
        name: 'SystemSettings',
        path: '/system/settings',
        component: () => import('#/views/system/settings/index.vue'),
        meta: { icon: 'lucide:settings', title: $t('page.system.settings'), authority: ['system:read'] },
      },
      {
        name: 'SystemAbout',
        path: '/system/about',
        component: () => import('#/views/_core/about/index.vue'),
        meta: { icon: 'lucide:copyright', title: $t('page.system.about'), authority: ['system:read'] },
      },
    ],
  },
];

export default routes;
