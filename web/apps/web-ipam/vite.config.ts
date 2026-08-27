import { defineConfig } from '@vben/vite-config';

export default defineConfig(async () => {
  return {
    application: {},
    vite: {
      server: {
        proxy: {
          '/api': {
            changeOrigin: true,
            rewrite: (path) => path.replace(/^\/api/, ''),
            // 本地调试主通道：dev server 代理到容器/宿主的 control-plane
            // （调试循环见 docs/dev 或 M5-001 卡：改前端零镜像重建）
            target: 'http://127.0.0.1:8443/api',
            ws: true,
          },
        },
      },
    },
  };
});
