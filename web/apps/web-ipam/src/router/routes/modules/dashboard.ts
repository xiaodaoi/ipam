import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:layout-dashboard',
      order: -1,
      title: $t('page.dashboard.title'),
    },
    name: 'Dashboard',
    path: '/dashboard',
    children: [
      {
        name: 'IpamOverview',
        path: '/overview',
        component: () => import('#/views/dashboard/ipam/index.vue'),
        meta: {
          affixTab: true,
          icon: 'lucide:gauge',
          title: $t('page.dashboard.ipamOverview'),
        },
      },
    ],
  },
];

export default routes;
