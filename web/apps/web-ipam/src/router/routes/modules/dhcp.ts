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
        name: 'DhcpOptions',
        path: '/dhcp/options',
        component: () => import('#/views/dhcp/options/index.vue'),
        meta: { icon: 'lucide:sliders-horizontal', title: $t('page.dhcp.options') },
      },
      {
        name: 'Dualstack',
        path: '/dhcp/dualstack',
        component: () => import('#/views/dhcp/dualstack/index.vue'),
        meta: { icon: 'lucide:layers', title: $t('page.dhcp.dualstack') },
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