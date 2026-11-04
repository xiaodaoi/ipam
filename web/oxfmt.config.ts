import { defineConfig } from '@vben/oxfmt-config';

export default defineConfig({
  ignorePatterns: [
    'dist',
    'dev-dist',
    '.local',
    '.claude',
    '.agent',
    '.agents',
    '.codex',
    '.output.js',
    'node_modules',
    '.nvmrc',
    'coverage',
    'CODEOWNERS',
    '.nitro',
    '.output',
    '**/*.svg',
    '**/*.sh',
    'public',
    '.npmrc',
    '*-lock.yaml',
    'skills-lock.json',
    // 生成物：由 scripts/gen-openapi.sh / scripts/gen-offline-icons.mjs 产出，
    // 参与格式化会造成「重新生成即产生 diff」，故排除（M6-027）。
    'apps/web-ipam/src/api/schema.d.ts',
    '**/*.generated.ts',
  ],
});
