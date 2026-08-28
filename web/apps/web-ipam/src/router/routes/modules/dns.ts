import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:server',
      order: 3,
      title: $t('page.dns.title'),
    },
    name: 'DnsService',
    path: '/dns-service',
    children: [
      {
        name: 'DnsUpstream',
        path: '/dns-service/upstreams',
        component: () => import('#/views/dns/upstream/index.vue'),
        meta: { icon: 'lucide:globe', title: $t('page.dns.upstream') },
      },
      {
        name: 'DnsForward',
        path: '/dns-service/forward',
        component: () => import('#/views/dns/forward/index.vue'),
        meta: { icon: 'lucide:route', title: $t('page.dns.forward') },
      },
      {
        name: 'DnsRecords',
        path: '/dns-service/records',
        component: () => import('#/views/dns/records/index.vue'),
        meta: { icon: 'lucide:book-open', title: $t('page.dns.records') },
      },
      {
        name: 'DnsCache',
        path: '/dns-service/cache',
        component: () => import('#/views/dns/cache/index.vue'),
        meta: { icon: 'lucide:gauge', title: $t('page.dns.cache') },
      },
      {
        name: 'DnsSecurity',
        path: '/dns-service/security',
        component: () => import('#/views/dns/security/index.vue'),
        meta: { icon: 'lucide:shield-check', title: $t('page.dns.security') },
      },
      {
        name: 'DnsBlocklist',
        path: '/dns-service/blocklist',
        component: () => import('#/views/dns/blocklist/index.vue'),
        meta: { icon: 'lucide:shield-ban', title: $t('page.dns.blocklist') },
      },
    ],
  },
];

export default routes;
