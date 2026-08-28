import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:scroll-text',
      order: 4,
      title: $t('page.logs.title'),
    },
    name: 'LogCenter',
    path: '/logs-center',
    children: [
      {
        name: 'LogSearch',
        path: '/logs-center/search',
        component: () => import('#/views/logs/search/index.vue'),
        meta: { icon: 'lucide:search', title: $t('page.logs.search') },
      },
      {
        name: 'LogAudit',
        path: '/logs-center/audit',
        component: () => import('#/views/logs/audit/index.vue'),
        meta: { icon: 'lucide:shield-check', title: $t('page.logs.audit') },
      },
    ],
  },
];

export default routes;
