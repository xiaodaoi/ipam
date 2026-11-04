import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    name: 'Onboarding',
    path: '/onboarding',
    component: () => import('#/views/_core/onboarding/index.vue'),
    meta: {
      hideInMenu: true,
      icon: 'lucide:rocket',
      title: $t('page.onboarding.title'),
    },
  },
];

export default routes;
