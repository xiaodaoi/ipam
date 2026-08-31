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
        meta: { icon: 'lucide:network', title: $t('page.dhcp.subnets'), authority: ['dhcp:read'] },
      },
      {
        name: 'Reservations',
        path: '/dhcp/reservations',
        component: () => import('#/views/dhcp/reservations/index.vue'),
        meta: { icon: 'lucide:pin', title: $t('page.dhcp.reservations'), authority: ['dhcp:read'] },
      },
      {
        name: 'DhcpOptions',
        path: '/dhcp/options',
        component: () => import('#/views/dhcp/options/index.vue'),
        meta: { icon: 'lucide:sliders-horizontal', title: $t('page.dhcp.options'), authority: ['dhcp:read'] },
      },
      {
        name: 'Dualstack',
        path: '/dhcp/dualstack',
        component: () => import('#/views/dhcp/dualstack/index.vue'),
        meta: { icon: 'lucide:layers', title: $t('page.dhcp.dualstack'), authority: ['dhcp:read'] },
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