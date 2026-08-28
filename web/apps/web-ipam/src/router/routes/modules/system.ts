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
        meta: { icon: 'lucide:network', title: $t('page.system.orgs') },
      },
    ],
  },
];

export default routes;
