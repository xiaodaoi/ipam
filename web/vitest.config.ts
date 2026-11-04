import Vue from '@vitejs/plugin-vue';
import VueJsx from '@vitejs/plugin-vue-jsx';
import { configDefaults, defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [Vue(), VueJsx()],
  test: {
    environment: 'happy-dom',
    pool: 'forks',
    // 单 worker：多个业务用例共享内存态 mock，避免并行 fork 互相干扰。
    maxWorkers: 1,
    environmentOptions: {
      happyDOM: {
        settings: {
          // happy-dom v20+ disables JS evaluation by default (security fix).
          // Treat disabled script loading as success to preserve test behavior.
          handleDisabledFileLoadingAsSuccess: true,
        },
      },
    },
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      // 组件覆盖率按「已接入 Vue Test Utils 用例的展示型组件」纳入；
      // 新增组件补测后把文件加入 include，避免未测组件稀释门槛。
      include: [
        'apps/web-ipam/src/utils/*.ts',
        'apps/web-ipam/src/components/metric-ring.vue',
        'apps/web-ipam/src/components/page-header.vue',
        'apps/web-ipam/src/components/stat-card.vue',
        'apps/web-ipam/src/components/trend-chart.vue',
      ],
      exclude: ['**/*.test.ts', 'apps/web-ipam/src/utils/site-branding.ts'],
      thresholds: {
        // 纯函数工具：高门槛，回归成本低。
        'apps/web-ipam/src/utils/*.ts': {
          branches: 70,
          functions: 80,
          lines: 80,
          statements: 80,
        },
        // 展示型组件：已有 Vue Test Utils 用例的逐个门槛。
        'apps/web-ipam/src/components/{metric-ring,page-header,stat-card,trend-chart}.vue':
          {
            branches: 70,
            functions: 75,
            lines: 75,
            statements: 75,
          },
      },
    },
    exclude: [
      ...configDefaults.exclude,
      '**/e2e/**',
      '**/dist/**',
      '**/.{idea,git,cache,output,temp}/**',
      '**/node_modules/**',
      '**/{stylelint,eslint}.config.*',
      '**/{oxfmt,oxlint}.config.*',
    ],
  },
});
