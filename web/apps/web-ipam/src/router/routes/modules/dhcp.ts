import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:network',
      order: 1,
      title: $t('page.dhcp.title'),
    },
    name: 'Dhcp',
    path: '/dhcp',
    children: [
      {
        name: 'DhcpSubnets',
        path: '/dhcp/subnets',
        component: () => import('#/views/dhcp/subnets/index.vue'),
        meta: { icon: 'lucide:network', title: $t('page.dhcp.subnets') },
      },
      {
        name: 'Ledger',
        path: '/dhcp/ledger',
        component: () => import('#/views/dhcp/ledger/index.vue'),
        meta: {
          icon: 'lucide:table',
          title: $t('page.dhcp.ledger'),
        },
      },
    ],
  },
];

export default routes;