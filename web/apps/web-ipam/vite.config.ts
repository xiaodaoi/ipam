import { defineConfig } from '@vben/vite-config';

export default defineConfig(async () => {
  return {
    application: {},
    vite: {
      build: {
        // IPv6 容量/利用率用 BigInt 精确计算（2^128），需要 ES2020 输出目标；
        // 默认 es2015 会把 BigInt 字面量降级并产生构建警告。
        target: 'es2020',
      },
      server: {
        proxy: {
          '/api': {
            changeOrigin: true,
            rewrite: (path) => path.replace(/^\/api/, ''),
            // 本地调试主通道：dev server 代理到容器/宿主的 control-plane。
            // 开发态前端 base 为 /api，生产为 /api/v1，这里补齐 /v1 前缀，
            // 避免登录、用户信息等请求落到后端未注册的 /api/* 路径。
            // （调试循环见 docs/dev 或 M5-001 卡：改前端零镜像重建）
            target: 'http://127.0.0.1:8443/api/v1',
            ws: true,
          },
        },
      },
    },
  };
});
