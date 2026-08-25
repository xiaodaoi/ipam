# 项目进度日志

> 格式：倒序追加。每次会话收尾必须在此追加一条（对应 AGENTS.md 纪律 3-b），内容=做了什么/改动范围/验证结果/遗留事项。

<!-- 新条目插入到本行下方 -->

## 2026-08-25 · M0-006 compose 骨架（验证交由 CI 闸⑥）

- **做了**：compose 双服务（PG16 健康检查+§3 八表迁移挂载 / control-plane 三阶段镜像）；install.sh 预检+冒烟；闸⑥解除自跳过，本次推送即首次真实验证。
- **验证结果**：YAML/Shell 语法通过；运行时冒烟由 Actions ubuntu-latest docker 执行。
- **遗留**：DSN→handler 探针接线在 M1/M2；TLS 留 M5。

## 2026-08-25 · M0-005 验收通过：CI 六道闸全绿

- **做了**：修复 golangci-lint 首跑两处 staticcheck（GetSpec 替换弃用 API、QF1007 条件合并）；二次运行 success（32815471234）。
- **验证结果**：api-lint/go-ci/web-ci/compose-smoke 四 job 全绿；闸②路由覆盖率测试随 go-ci 常驻生效。
- **里程碑状态**：M0 进度 88%，仅剩 M0-006 compose 骨架（需 docker 环境实施与验证）。

## 2026-08-25 · M0-005 CI 六道闸落地

- **做了**：GitHub Actions 五 job（api-lint/go-ci/openapi-diff/web-ci/compose-smoke）对应 §12.4 六闸；闸②固化为 Go 测试 TestRoutesCoveredBySpec；.spectral.yaml 规则集（examples 强制+驼峰 operationId）；.golangci.yml；Makefile lint-api。
- **验证结果**：本地 spectral 无 error、go test 4 用例全绿；工作流已推送，首跑跟踪中。
- **踩坑**：spectral 内置规则名差异；kin-openapi 大写方法名；CI stub webui 策略。
- **遗留**：Actions 首跑结果确认后卡片转 done；main 分支保护待网页启用。

## 2026-08-25 · M0-008 embed 打通：单二进制全链路贯通

- **做了**：webui embed 包+同步脚本（接入 make build 管线）；NoRoute 三分支路由（API Problem 化/静态回写/SPA fallback）；离线零外链断言脚本；fallback 单测 3 例。
- **验证结果**：build/vet/test 绿；冒烟 / 与深路由 200 html、API json、404 problem；离线断言 PASS（产物 3.9M）；`make build` 一键产出 27M 单二进制。
- **踩坑**：gin FileFromFS 对根路径 301 → 改 c.Data 直写；embed 空目录编译失败 → 被跟踪 .gitkeep 兜底。
- **发现**：⚠️ Iconify 在线图标服务字符串（运行时拉取风险），P2 用 unplugin-icons/@iconify/json 离线化加固。

## 2026-08-25 · M0-007 Vben Admin v5.7.0 底座引入与裁剪

- **做了**：锁定 v5.7.0 引入 web/；按官方精简指南删除 4 个备用 UI 应用/backend-mock/playground/docs；web-antd→web-ipam 改造；根 scripts 同步。
- **验证结果**：pnpm install(2m36s)/build:ipam(11 tasks)/typecheck 三绿；packages/** 与上游源码零差异（禁改区基线确立）。
- **踩坑**：Node26 无 corepack → npm 用户态装 pnpm@10；mv 嵌套目录陷阱已纠正。
- **遗留**：登录 Bearer 对接与 TS 客户端页面接入（补 M0-004 前端腿）→ M0-008。

## 2026-08-25 · M0-004 后端腿：OpenAPI→Gin 端到端贯通

- **做了**：Go1.27 用户态安装+GOPROXY 切 goproxy.cn；仓库首个 spec（GET /api/v1/system/info，§12.2 全要素示范）；oapi-codegen v2.8.0 生成 Gin 接口；platform.Handler + WriteProblem（RFC9457）；main.go 装配 :8443。
- **改动范围**：api/openapi、api/gen/go、internal/module/platform、cmd/control-plane、go.mod/sum、LICENSES.md。
- **验证结果**：build/vet/test(3) 全绿；gen-check 一致性 OK；二进制冒烟 curl 返回正确 JSON。
- **踩坑**：oapi 配置键 generate:；v2.8 前缀走 GinServerOptions.BaseURL（已留痕卡片作模板）。
- **遗留**：TS 客户端与页面调用并入 M0-007/M0-008 补验；golangci-lint 入 M0-005 CI；PG 探针接线在 M0-006。

## 2026-08-25 · M0-003 Makefile 统一构建入口落地

- **做了**：Makefile（help/doctor/build/test/lint/gen/gen-check/clean）薄封装化；三个支撑脚本——make-part.sh 分部执行器、gen-openapi.sh 再生+一致性门禁、doctor.sh 九项工具自检（--strict CI 模式）；AGENTS.md 命令节转正。
- **改动范围**：Makefile、scripts/×3、AGENTS.md；卡片 backlog→doing→review。
- **验证结果**：四路守卫 skip 且 rc=0；doctor 报告与实际工具链一致；strict 模式缺硬依赖正确退出 1。
- **环境事实**：沙箱无 make/root，验证走 scripts 直调路径（已写入 AGENTS.md 回退说明）。
- **遗留**：gen 端到端一致性待 M0-004 spec 就绪补跑；shellcheck 列入 M0-005 CI。

## 2026-08-25 · M0-002 验收通过

- review→done（人工确认）；M0 进度 2/8。

## 2026-08-25 · M0-002 仓库目录骨架落地

- **做了**：领取 M0-002 任务卡（backlog→doing→review）；按 §14 创建全量目录树并配 README；新增 `LICENSES.md` 许可矩阵初版与 `docs/README.md`。
- **改动范围**：39 个目录路径 + 13 个一级 README + LICENSES.md；无代码。
- **验证结果**：逐项比对脚本输出 `VERIFY-OK`，与 §14 无缺漏。
- **遗留**：web/、api/gen 空壳待 M0-007/M0-004 填充；卡片在 review 列待人工确认转 done。

## 2026-08-25 · Git 远端接入 GitHub

- **做了**：生成 ed25519 SSH 密钥，远端切换 `git@github.com:xiaodaoi/ipam.git`，main 推送成功并建立跟踪（464ee7d）。
- **遗留**：建议网页端启用 main 分支保护（PR 必审）。

## 2026-08-25 · 项目管理地基落盘（M0 前置）

- **做了**：git 初始化（main 分支）；建立强项目管理资产——根 AGENTS.md 协作纪律、`.opencode/agent/` 四角色代理、`docs/pm/`（路线图 roadmap、风险登记 risks、进度日志 progress-log、四列看板 board/ 与任务卡模板）；生成 M0 任务卡 8 张入 backlog；`.gitignore`（密钥/构建产物忽略）。
- **改动**：`AGENTS.md`、`.opencode/agent/*.md`×4、`docs/pm/**`、`.gitignore`。
- **验证**：文件树结构核对通过。
- **遗留**：全部 M0 任务待领取；git 身份未配置全局，首次 commit 使用内联身份。
