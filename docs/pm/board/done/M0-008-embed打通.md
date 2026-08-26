# [M0-008] embed 打通（前端产物 → :8443）

| 字段 | 内容 |
|---|---|
| ID | M0-008 |
| 状态 | done |
| 来源 | §13.3、架构图 L34 |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

web-ipam 构建产物输出到 cmd/control-plane/webui/dist 并 go:embed；Gin SPA fallback（NoRoute→index.html）；浏览器访问 :8443 出完整页面并调通 M0-004 接口。

## 验收标准

- [x] 单二进制运行，无外部静态文件依赖（bin/control-plane 27M，含完整 UI）
- [x] 刷新深层路由不 404（/dashboard/analytics → 200 text/html，fallback 生效）
- [x] scripts/ 内零外链资产断言脚本就位（check-webui-offline.sh，PASS）
- [x] API 未匹配路由 Problem 化（404 + application/problem+json + 业务码，§12.2）
- [x] `make build` 管线闭环：web build → sync-webui → go build 一键产出

## 涉及模块

- cmd/control-plane/{main.go,main_test.go,webui/{embed.go,dist/.gitkeep}}
- scripts/{sync-webui.sh,check-webui-offline.sh}、scripts/make-part.sh（build 接入 sync）、Makefile

## DoD 自检

- [x] fallback 单测 3 例：根路径 index / 深路由回退 / API 404 problem
- [x] lint：go vet 绿；typecheck 前置已在 M0-007 通过
- [x] 文档同步：webui/embed.go 与脚本注释即文档；无架构偏差
- [x] spec N/A　- [x] commit 带 `[M0-008]`

## 实施记录

### 2026-08-25 · 会话1

- **做了**：webui embed 包（all:dist + fs.Sub）；sync-webui.sh 同步脚本（保留 .gitkeep 保证 fresh clone 可编译）并接入 make-part build；NoRoute 三分支路由（API→Problem / 静态命中→c.Data 回写 / 其余→index.html）；离线断言脚本（精确检测加载型外链：html src/href、CSS url()/@import，JS 字符串 URL 仅 INFO）。
- **踩坑留痕**：① gin `c.FileFromFS` 对 "/" 触发 301 重定向——改 `fs.ReadFile + c.Data` 直写绕开 http.FileServer 路径清洗；② go:embed 在 fresh clone 无 dist 时编译失败——以被跟踪的 dist/.gitkeep 兜底。
- **验证结果**：go test cmd 全过；冒烟 `/`=200 html、深路由=200 html、API=200 json、API404=problem+json；check-webui-offline PASS。
- **发现与遗留**：
  1. ⚠️ JS 内存在 `api.iconify.design/simplesvg/unisvg` 图标在线服务字符串——vben Iconify 对未打包图标会**运行时在线拉取**，内网弱网下新图标可能不渲染。加固方案（P2）：unplugin-icons 本地打包所用图标集或引入 @iconify/json 全量离线包；
  2. demo 页残留 avatar.vercel.sh 等字符串（仅演示组件引用），随真实页面替换消除；
  3. M0-004 前端腿（TS 客户端+页面调用展示）仍待专项实施——建议下一卡完成，届时两卡同转 done。

### 2026-08-26 · 批量验收

- 用户确认通过，review→done。
