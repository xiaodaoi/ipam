import Vue from '@vitejs/plugin-vue';
import VueJsx from '@vitejs/plugin-vue-jsx';
import { configDefaults, defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [Vue(), VueJsx()],
  test: {
    environment: 'happy-dom',
    pool: 'forks',
    poolOptions: { forks: { singleFork: true } },
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
      include: ['apps/web-ipam/src/utils/*.ts'],
      exclude: ['**/*.test.ts', 'apps/web-ipam/src/utils/site-branding.ts'],
      thresholds: {
        branches: 70,
        functions: 80,
        lines: 80,
        statements: 80,
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
