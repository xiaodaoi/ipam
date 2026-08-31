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
        meta: { icon: 'lucide:network', title: $t('page.system.orgs'), authority: ['system:read'] },
      },
      {
        name: 'SystemUsers',
        path: '/system/users',
        component: () => import('#/views/system/users/index.vue'),
        meta: { icon: 'lucide:users', title: $t('page.system.users'), authority: ['system:read'] },
      },
      {
        path: '/system/roles',
        component: () => import('#/views/system/roles/index.vue'),
        meta: { icon: 'lucide:shield-check', title: '角色管理', authority: ['system:read'] },
      },
      {
        path: '/system/settings',
        component: () => import('#/views/system/settings/index.vue'),
        meta: { icon: 'lucide:settings', title: '系统设置', authority: ['system:read'] },
      },
    ],
  },
];

export default routes;
