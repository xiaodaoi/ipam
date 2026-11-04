import { defineConfig } from '@vben/eslint-config';

export default defineConfig([
  {
    // 生成物：由 `make gen`（api/gen + schema.d.ts）与 `make gen-icons` 产出。
    // 参与 lint/格式化会在「重新生成」时产生无意义 diff，故整体排除（M6-027）。
    ignores: [
      'apps/web-ipam/src/api/schema.d.ts',
      'apps/web-ipam/src/icons/*.generated.ts',
    ],
  },
  {
    // 模块增强文件需要 `export {}` 才能保持「模块」语义（否则 `declare module`
    // 会整体替换目标模块类型，TS 报 has no exported member），故关闭该规则（M6-027）。
    files: ['**/*.d.ts'],
    rules: { 'unicorn/require-module-specifiers': 'off' },
  },
  {
    // 换行/缩进等排版统一交给 oxfmt（`vsh lint` 中先于 eslint 执行），
    // eslint 不再重复格式化，避免两套规则互相回滚（M6-027）。
    rules: {
      'vue/html-closing-bracket-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      // 这两条同为排版规则，且与 oxfmt 的规范化结果相反（括号增删、hex 大小写），
      // 同时开启会导致「eslint --fix → oxfmt → eslint」无限回滚。
      'unicorn/no-nested-ternary': 'off',
      'unicorn/number-literal-case': 'off',
    },
  },
  {
    // 上游底座遗留：pnpm catalog 保留了大量模板/文档站可选依赖条目，
    // 与本产品依赖无关；清理会牵连上游升级路径，故在门禁中关闭这两条（M6-027）。
    files: ['pnpm-workspace.yaml', 'package.json'],
    rules: {
      'pnpm/json-enforce-catalog': 'off',
      'pnpm/yaml-no-unused-catalog-item': 'off',
    },
  },
]);
