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
        meta: { icon: 'lucide:search', title: $t('page.logs.search'), authority: ['logs:read'] },
      },
      {
        name: 'LogTail',
        path: '/logs-center/tail',
        component: () => import('#/views/logs/tail/index.vue'),
        meta: { icon: 'lucide:radio', title: $t('page.logs.tail'), authority: ['logs:read'] },
      },
      {
        name: 'LogAudit',
        path: '/logs-center/audit',
        component: () => import('#/views/logs/audit/index.vue'),
        meta: { icon: 'lucide:shield-check', title: $t('page.logs.audit'), authority: ['logs:read'] },
      },
    ],
  },
];

export default routes;
