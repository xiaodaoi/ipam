// M6-018：注册离线 iconify 图标集合。
//
// 由 scripts/gen-offline-icons.mjs 从源码扫描 + @iconify/json 提取子集生成，
// 在应用启动最早期 addCollection 注册，使 IconifyIcon 直接命中本地集合，
// 不再访问 api.iconify.design 外部 CDN（内网/离线可用，CSP 无需放行公网域）。
import { addCollection } from '@vben/icons';

import { offlineIconCollections } from './offline-icons.generated';

type IconifyCollection = Parameters<typeof addCollection>[0];

for (const collection of offlineIconCollections) {
  addCollection(collection as IconifyCollection);
}
