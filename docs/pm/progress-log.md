# 项目进度日志

> 格式：倒序追加。每次会话收尾必须在此追加一条（对应 AGENTS.md 纪律 3-b），内容=做了什么/改动范围/验证结果/遗留事项。

<!-- 新条目插入到本行下方 -->
## 2026-08-28 · M2-017 完成：保留与绑定页 + Pools 回填缺陷修复

- **做了**：bulkReservations client + 三卡页（批量创建/保留列表/绑定列表，后端零改动）；DHCP 菜单「保留与绑定」child。
- **诊断战果**：联调发现台账全量 0 行 → 逐层实证定位 **PgSubnetRepo List/Get 不回填 Pools** 既有缺陷（台账 0 行 + kea 空池双影响）→ loadPools 修复。
- **验证结果**：全链测试绿 + lint 0 + 零外链；容器 e2e——pools 有值、台账 100 行、reserved 过滤精确 4 条、bulk 事务回滚语义验证。
- **里程碑**：主导航 14 页；DHCP 菜单 5/6（缺 DHCPv6 PD 委派 P2）。
- **遗留 → M3-007 立卡**：bind 配置式下发（host_cmds 缺失 + 命令式重载丢失 + bulk 回滚 bind 缺口）P1。
- **运维事件**：构建缓存堆满磁盘（ENOSPC，postgres unhealthy 同根因）→ builder/image prune 释放 58G。
## 2026-08-28 · M2-016 完成：DHCP 选项与类匹配页（Kea 真实下发打通）

- **做了**：/dhcp/options+/dhcp/classes CRUD（闸① 0 errors）；迁移 0011 自动应用（M5-005 runner 首秀）；internal/module/dhcp 启用；kea BuildConfigFull 注入 option-data/client-classes；前端 DHCP 菜单双卡页。
- **验证结果**：全链测试绿 + lint 0 + 零外链 PASS；容器 e2e——合法配置被 Kea 原子接受（无错误日志），非法配置结构化软失败（X-Kea-Warning + 数据落库）。
- **里程碑**：**Kea 真实下发链路打通**——修复 ctrl.go RealApply 同源 bug（Dhcp4 包裹缺失，子网真实下发此前也会失败）；迁移 runner 首个增量实战（0011 零手工）。
- **遗留**：Kea 自定义选项定义 P2；eval 语法前端预校验 P2；DHCP 菜单最后 1 页待确认（§13.4 第 2 项）。
## 2026-08-28 · M5-005 完成：迁移 runner

- **做了**：控制面启动自动应用增量迁移（schema_migrations 记账 + 存量库基线 + 简单协议多语句执行）；compose 只读挂载 + dev.sh 注入；未配置目录跳过。
- **验证结果**：单测 3 组绿 + lint 0；容器 e2e 三段全过（首启基线/增量 drop→重建播种/修复后零重放）。
- **里程碑**：新增迁移（0011+）零手工介入；M5-004 的手动 psql 坑正式填平。
- **遗留**：非幂等迁移在部分初始化卷上失败即 fatal（快速暴露，可接受）。
## 2026-08-28 · M5-004 完成：用户与角色管理

- **做了**：登录从 PoC 硬编码升级为 users 表真实账号（bcrypt + DB 角色签发 claims）；/users CRUD + 自保护与最后一名 admin 守卫；前端系统管理菜单用户页（创建/重置密码/启停/角色）。
- **验证结果**：6 组新单测全绿 + lint 0 + 零外链 PASS；容器 e2e 8 项全过（user 角色写操作 403 RBAC、admin 自改角色 400、禁用账号登录 401）。
- **里程碑**：主导航 13 页（系统管理：组织管理 + 用户与角色）；RBAC 从"写拦截"落地为"真实账号驱动"。
- **遗留**：存量令牌无吊销 P2；迁移 runner（既有卷增量迁移）P1 建议立卡。
## 2026-08-28 · M2-014 完成：解析测试台真实现

- **做了**：spec /dns/diagnose + gen（闸① 0 errors）；后端 miekg/dns 查询（PTR 直填 IP 便利形态、业务失败结构化 200）；前端安全页真表单+答案表；LICENSES.md 登记 miekg/dns（BSD-2）。
- **验证结果**：单测 5 组绿+lint 0+零外链 PASS；容器 e2e——example.com A 查询真实递归 NOERROR（2 答案 ttl300），PTR 超时结构化返回。
- **里程碑**：DNS 菜单 6/6 全部真实现；安全诊断页从占位毕业。
- **顺手治理**：清理误入暂存区的根目录 control-plane 二进制与 .env.*（红线：环境文件不入库，.gitignore 已补）。
## 2026-08-28 · M2-013 完成 + 调试循环建立（make dev）

- **M2-013**：daemon 从 PG prefix_template 动态装载（TplLoader 缓存+30s 刷新+失败保旧缓存）；e2e 日志 `loaded 1 templates from PG`。双栈页→联动闭环打通。
- **用户报障 401**：ipam.ts req() 裸 fetch 不带 Authorization → 全部写操作 401 TOKEN_MISSING。修复：接入 accessStore 携带 bearer；错误 detail 透出（不再只显示"添加失败"）。
- **调试循环**：make dev——宿主跑 control-plane（IPAM_WEBUI_DIR 磁盘 dist / IPAM_HTTP_ADDR），容器只跑依赖（PG/CH 发布 127.0.0.1 回环端口）；make dev 附 Vite HMR（--web）。30s 从零到可登录，免镜像构建；Dockerfile 加 go 缓存挂载提速正式构建。
## 2026-08-28 · M3-001 修复：上游「添加」按钮无效（用户报障）

- **根因**：Create 上游在 unbound 下发失败时返回 503（数据已落库），前端 add() 无 try/catch → 表单不重置、列表不刷新，看起来"点了没反应"。
- **修复**：后端 soft-fail（201 + X-Unbound-Warning 头，对齐 Update/Delete 语义）；前端 add()/remove() 加反馈并始终刷新列表。
- **验证**：容器端到端 POST→201+warning 头，列表即时可见；测试绿 lint 0。
## 2026-08-28 · M2-012 完成：双栈管理页+prefix_template HTTP CRUD

- **做了**：发现 prefix_template 无 HTTP API 缺口→spec-first 补 /dualstack/templates CRUD；dualstack 模块（Mem/PG 双 Store+Coherence 投影）；前端双栈管理页挂 DHCP 菜单。
- **验证结果**：2 组单测+全仓绿+lint 0；容器端到端 POST 创建（PG RETURNING id）+list+SPA 200。
- **里程碑**：主导航可用页面 11 个；DHCP 菜单 4/6（子网/台账/双栈/台账重叠）。
- **遗留**：daemon 动态装载 PG 模板 P1（联动实效关键）；模板编辑 P1。
## 2026-08-28 · M2-011 完成：DNS 缓存性能+安全诊断页——§13.4 DNS 六菜单前端全齐

- **做了**：缓存与性能页（TTL/serve-expired 表单、flush、每域名 TTL 覆盖）+ 安全与诊断页（RRL/DNSSEC 占位/测试台占位）；DNS 菜单六子页齐。
- **验证结果**：typecheck/build/零外链绿；部署后两路由 200，settings 实测读取默认参数。
- **里程碑**：前端可用页面 12 个（仪表盘/台账/子网/日志×3/DNS×6/组织）。主导航 §13.4 覆盖度 12/17。
- **遗留**：解析测试台真实现 P1；DNSSEC P2；DHCPv6 管理/选项类匹配/双栈管理页与用户角色页未开。
## 2026-08-28 · M2-010 完成：组织管理页（主数据闭环）

- **做了**：/system/orgs 页——树展示/根+子新建/改名/删除（409 引用保护弹出）；「系统管理」菜单首子页；RBAC 上线后首次全流程带令牌实测。
- **验证结果**：typecheck/build/零外链绿；容器实测组织 CRUD+409 保护+演示树（总公司→研发部）灌入。
- **里程碑**：组织主数据闭环——台账/子网页的组织筛选从此有真实数据源；§13.4「单一事实源」原则前端侧达成。
- **遗留**：拖拽移动 P1；组织详情面板 P2。
## 2026-08-28 · M2-009 完成：DHCP 子网与池管理页

- **做了**：子网页（组织树筛选+新建表单下发 Kea+keaSubnetId 回显+删除）；api/ipam.ts 扩 subnets CRUD 与 /orgs 树接口。
- **验证结果**：typecheck/build/零外链绿；容器部署 /dhcp/subnets 200。
- **踩坑**：sed/python 消重脚本误清空 ipam.ts（git checkout 秒恢复；教训=脚本编辑前后必须 wc 校验）。
- **里程碑**：主导航可用页面 10 个（仪表盘/台账/子网/日志×3/DNS×4）。
## 2026-08-28 · M2-008 完成：DNS 管理前端批次（四页）

- **做了**：「DNS 服务」一级菜单四子页——上游管理（探活灯/RTT/15s 刷新）、转发规则（域名后缀→上游）、解析记录（zone 切换/静态 CRUD/联动只读 tab）、封禁管控（名单/feed 同步）；api/ipam.ts 扩 12 函数。
- **验证结果**：typecheck/build/零外链 PASS；golangci 0。
- **里程碑**：§13.4 主导航可用页面 7/17（仪表盘 1+DHCP 1+日志中心 3+DNS 4 减重叠…精确=台账+仪表盘+日志×3+DNS×4=9 页）。
- **遗留**：缓存与性能/安全诊断页 P1；封禁策略分组视图 P1。
## 2026-08-28 · CI 七道闸首次全绿（run 33142407177）+ gitignore 陷阱修复

- **根因链**：web/.gitignore 裸词 `logs` 误伤 src/views/logs 三页面未入库 → CI TS2307 三模块缺失。本地 typecheck 全过（文件存在），仅 CI 缺文件——"本地绿 CI 红"的经典形态。
- **修复**：gitignore 收窄为 `/logs`（日志产物语义）；三个 .vue 页面入库。
- **CI 编排升级（同批落地）**：web-ci 上传 dist artifact → compose-smoke needs 串行下载 → control-plane 走 Dockerfile.prebuilt（无 node 阶段，unbound 二进制复用 ipam/unbound 镜像产物）→ up --no-build。彻底解决 runner 双 node 构建并行 OOM。
- **验证结果**：run 33142407177 = api-lint ✓ go-ci ✓ hook-ci ✓ web-ci ✓ compose-smoke ✓（K1~[7] 全断言）。openapi-diff 为 PR-only skip。
- **里程碑**：**CI 六闸首次端到端全绿**。自 M0-005 建闸以来所有历史红项（compose 卷声明/hook 命名空间/gitignore/web OOM/vector 镜像/CH 语法）全部闭环。
## 2026-08-28 · M5-003 完成：RBAC 写权限拦截

- **做了**：NewRBACMiddleware——变更类请求强制 admin JWT（无令牌 401/user 角色 403 FORBIDDEN/login 白名单放行），装配于审计中间件之前（被拒请求不入账）。
- **验证结果**：5 组单测+容器实测（admin POST 201/无令牌 401/GET 不受限）；lint 0/全仓 test 绿。
- **里程碑**：认证授权语义完整（鉴别=JWT、授权=RBAC、审计=真实身份），§12.3 三要素闭环。
- **遗留**：端点级细粒度 scope P2；多用户/Bot Token 管理界面 P1。
## 2026-08-28 · live-tail 页落地 + vector type 语义修复 + SSE 时序验证方法论

- **做了**：live-tail 页（/logs-center/tail，EventSource 断线自动重连、类型过滤、倒序保留 200 条、连接状态 Tag）；vector .rtype 修复（unbound 行此前误存 qtype 'A'/'SOA'、notice 透传空 type——固定 'dns' + 未匹配 abort 丢弃）；SSE 无 from 回拨 5s。
- **验证结果**：SSE 端到端实测通过（curl -N 收含 id/data 的完整事件帧；实测确认事件到达延迟 8~15s=vector 批 5s+轮询+CH merge，非代码缺陷）；typecheck/build/零外链/lint 全绿。
- **踩坑**：VRL 对象字面量不能内联 if 表达式；字符串拼接动态类型全 fallible 须 err 解构或预变量；宿主 jammy glibc 2.35 无法运行 Playwright chromium build（需 2.39+）——浏览器 e2e 必须容器化。
- **里程碑**：日志中心三页齐（检索/实时流/审计），M2-007 完整交付。
- **遗留**：vector sink 偶发停摆探针 P1；echarts P1。
## 2026-08-27 · M2-007 完成：日志中心两页（检索+审计）

- **做了**：日志检索页（时间窗/type/domain 过滤、游标"加载更多"、TopN 域名 tab、QPS CSS 柱状曲线 30s 自刷新）+ 操作审计页（actorType/action/q 过滤、7 天窗、游标分页、人工/Bot 着色）；「日志中心」一级菜单两子页 + zh/en locale；api/ipam.ts 扩 4 个类型化客户端。
- **验证结果**：typecheck/build/零外链 PASS；golangci 0；容器重建后 SPA 路由 /logs-center/{search,audit} 200。
- **里程碑**：主导航四大可用区（仪表盘/DHCP 台账/日志中心×2）全部消费真实 API；日志中心 6 端点前端消费完毕。
- **遗留**：live-tail 页消费（M4-003 SSE）P1；echarts 曲线升级 P1。
## 2026-08-27 · 登录"内部服务器错误"根因修复（响应拦截器语义）+ 构建缓存治理

- **根因**：vben requestClient 默认 responseReturn='data'（mock 私约 {code:0,data}），后端 REST 裸 JSON 无 code 字段 → 登录成功响应被误判失败抛错；docker build cache 50GB 压垮 7G 内存宿主致构建卡死。
- **做了**：request.ts 改 responseReturn='body'（RFC9457 错误语义保持 401 拦截兼容）；builder prune 清 38GB；npm install -g pnpm 走阿里源（上一轮）。
- **验证结果**：容器重建后 login/user/info/dashboard 全 200、错误口令 401 正确分支；typecheck/build/零外链绿；commit 3064f3c。
- **里程碑**：Web 界面登录闭环打通（admin/admin123 → 仪表盘总览+地址台账可用）。
- **遗留**：浏览器 e2e 容器化（宿主 glibc 限制）；构建产物 hash 缓存需用户强刷一次。
## 2026-08-27 · M5-002 完成：正式 JWT + 审计真实身份接通

- **做了**：标准 HS256 JWT 替换 PoC 令牌（响应结构零变更，平滑升级）；claims sub/uid/roles/typ；审计 ActorProvider 从 JWT 解析 human/bot + token_sub 指纹落 operation_audit（M4-003 预留钩子接通）。
- **验证结果**：JWT 5 组单测（往返/过期/篡改/异密钥/alg 混淆）+ 全仓绿 + lint 0；容器实测 POST /orgs 201 → 审计表实记录 human|admin|jwt:admin#指纹。
- **里程碑**：审计的 §12.3 人/Bot 区分从 system 兜底变为真实数据；认证链路 M5-001/002 交付完毕。
- **遗留**：端点级 scope 强制拦截待 RBAC 中间件卡；Bot Token 管理界面与多用户管理 P1。
## 2026-08-27 · 登录过期弹窗根因修复（/user/info 别名）+ 构建提速三连

- **根因**：vben 登录成功后 fetchUserInfo 固定调 GET /user/info，后端只实现了 /auth/user/info → 401 → loginExpired 弹窗死循环（与 token 本身无关）。
- **做了**：① /user/info 别名端点（同资源零侵入底座约定，spec 声明）；② 构建提速：vite dev 代理指 127.0.0.1:8443（本地调试主通道，改前端零镜像重建）+ Dockerfile manifest-first 层序（38 包清单前置，install 层只被依赖变更击穿，--ignore-scripts 后置 stub）+ npm/pnpm 全走阿里源 + 去掉镜像内重复 typecheck。
- **验证结果**：全容器重建后 vben 调用序列全 200（login/user/info/codes/dashboard）；前端产物无外链；golangci 0 issues；镜像构建不再因前端单文件改动重装依赖。
- **遗留**：浏览器 e2e 容器化（宿主 glibc 不兼容 chromium-build）；M5-002 正式 JWT。
## 2026-08-27 · M5-001 完成：认证 PoC 直通（界面可登录）

- **根因定位**：用户反馈"登录后啥也没有"=生产前端 API 指向 vben 公网 mock + 后端无 /auth/* → 登录必然失败静默停留；index.html 百度统计外链违反离线契约。
- **做了**：auth 四端点 spec-first + HMAC 无状态令牌（poc.uid.exp.sig16，24h）+ fixed account admin/IPAM_POC_PASSWORD；.env.production 同源 /api/v1；移除统计注入。
- **验证结果**：契约级全流程实测通过（401 分支/防篡改/userInfo 对齐 vben UserInfo/codes/logout）；lint 0 issues/typecheck/build/零外链绿；容器重建后复测 OK。闸⑥ CI 首绿已达成，hook-ci 取证修正中。
- **遗留**：M5-002 正式 JWT/RBAC 替换签发实现并接 ActorProvider；浏览器 e2e 容器化列后续卡。

## 2026-08-27 · hook-C++ 编译修正 + vector 镜像钉版 ghcr 0.46.1（CI 终验前置）

- **hook-ci 根因**：ResolveClientMac 漏 ipam::coherence 命名空间限定——option79 落地时引入、CI 从未绿过 K4；沙箱内 cmake 复现→修复→ctest 100% 过（commit 5403b0d）。
- **vector 钉版**：Docker Hub `0.40-alpine` 二段 tag 不存在（官方三段式 `0.40.0-alpine`）且 CI 需确定性版本 → compose 改钉 `ghcr.io/vectordotdev/vector:0.46.1-alpine`（现役版，与本地全部 VRL 实证一致）；本地镜像市场拉错 arm64 的坑记录在案（daemon mirror 多架构干扰），CI 直连 ghcr 无此问题。

## 2026-08-27 · 首次全栈 compose 冒烟实证通过（闸⑥ 7 项断言本地全绿）+ M4 实测归档

- **做了**：本地 Docker 全栈起跑（12 容器 Healthy），逐项实证闸⑥ [1]~[7]：control-plane/kea/unbound/K1/local-data 热注/CH 检索 [6]/TopN MV [7]；连带修复八处环境与代码缺陷（详见 commit 9be85b4）：compose 卷声明、Dockerfile 换源剥 scheme、unbound chroot+zone SOA 缺失（K1 根因）、CH tokenbf 三参、vector 0.46 迁移+日志格式补 srcip 捕获、Go 驱动 UInt64 扫描、CI 秒级时间戳误用。
- **验证结果**：[1]~[7] PASS；vector→CH→/logs 域过滤端到端实测通（sip 落列 ::ffff 映射正确）；四组件健康灯全 up；lint/test 绿。commit 9be85b4 已推送，CI 将首次完整复现实证。
- **里程碑**：M0~M4 全部任务卡的"容器实测"验收路径打通——M1~M4 review 列 14 张卡可依此批量归档；日志链路三段（引擎日志→采集→查询）全部实证。
- **遗留**：组织树 PoC 数据未灌（orgId 过滤待演示数据）；水位采样 FR-E-05 P1。

## 2026-08-27 · CI 归因与 compose 卷声明修复

- **归因**：09c2903 轮 CI——api-lint/web-ci 绿；compose-smoke 挂在 `service "unbound" refers to undefined volume unbound-logs: invalid compose project`（ci-diag 分支实锤），**存量问题非 M4 引入**（7533703 同因）；hook-ci 失败=Kea dev 头文件真机依赖，CI 无法安装（M1-003 已知限制）。
- **修复**：compose.yaml 顶层 volumes 补 `unbound-logs:` 声明（dbba12f）；本地校验全部具名卷已定义。
- **遗留**：闸⑥ 全量断言（含 [6]/[7] CH 检索）待本轮 CI 实证；hook 真机编译验证列 M1 收尾项。

## 2026-08-27 · M4-004 完成：仪表盘聚合 API —— M4 日志中心里程碑收官

- **做了**：`GET /dashboard` 单端点聚合（活跃终端+24h 趋势+新增/离线+四组件健康灯+DNS QPS/拦截+池利用率 TopN+联动成功率）；logquery.Store 扩三个聚合口径双实现；前端总览页（/dashboard/overview）消费展示带 30s 自刷新。
- **验证结果**：dashboard 单测 3 组+全仓绿；lint 0 issues；web typecheck/build/零外链全绿；闸②覆盖。
- **踩坑**：oapi-codegen 把 required 内 nullable 拍平为值类型（改非 required 得指针）；组合结构体第三轮嵌入名冲突统一以命名包装类型处理；浮点断言需容差比较。
- **里程碑**：**M4 日志中心 4/4 全部代码交付**（采集链路/检索 API/实时流+审计/仪表盘聚合）。D6 PoC 核心闭环在代码侧就绪，待 compose 环境端到端实测。
- **遗留**：keepalived 健康灯无探测路径（unknown）；命中语义日志待 vector 增强。

## 2026-08-27 · M4-003 完成：实时流（SSE live-tail）+ 操作审计

- **做了**：`GET /logs/tail` SSE 流式端点（500ms 轮询滚动窗口、元组 id 续传、15s 心跳）+ `GET /audits` 审计检索 + operation_audit 迁移 0009 + 变更请求审计中间件（resource 归一路由模板）；spec 先行四闸自检通过。
- **验证结果**：新增 5 组单测（含 SSE smoke 110ms 抵达断言）+ 全仓绿；golangci-lint 0 issues；闸②自动覆盖新路由。
- **踩坑**：YAML 明文标量含 ": ping" 序列触发 mapping 解析错误（加引号解决）；pgx Rows.Close 无返回值与 clickhouse-go 有返回值在 errcheck 下行为不同；组合结构体第二轮名冲突由命名包装类型承接。
- **里程碑**：M4 日志中心 3/4（采集✓ 检索✓ 实时流+审计✓），仅余 M4-004 仪表盘聚合 backlog。
- **遗留**：actor 身份 M5 JWT 填充；Bot Token 类型与只读 scope P1。

## 2026-08-26 · M4-002 完成：日志检索 API（四端点+CH 查询层）

- **做了**：`/logs` `/logs/top` `/logs/qps` `/logs/export` spec 先行全流程；logquery 模块（ChStore 原生协议/MemStore 双实现、元组游标分页、组织 CIDR 合并区间+MAC IN 展开、CSV 导出）；PgOrgExpander 物化路径子树展开；logs.sql Nullable 列修正+TopN 物化视图；BuildConf/unbound.conf 补 logfile+log-queries（M4-001 遗留① 收口）；闸⑥ 扩展 CH 直插→检索断言。
- **验证结果**：全仓 go test 绿；golangci-lint 0 issues；web typecheck 绿；闸① spec lint 0 errors；万行级基准达标。
- **踩坑**：isIPAddressInRange 对 v6 列传 v4 CIDR 静默返回 0（应用层转 ::ffff/(96+n) 合并区间规避）；pgx Rows.Close 无返回值而 clickhouse-go 有，errcheck 差异；匿名结构体嵌入 platform.Handler 与 logq.Handler 字段名冲突需命名包装。
- **里程碑**：M4 日志中心 2/4（采集链路✓ 检索 API✓），遗留实时流/审计/仪表盘三卡 backlog。
- **已知限制**：DNS 事件无源 IP → 组织过滤仅命中 DHCP 事件，待 vector sip 提取。

## 2026-08-26 · 多池对联动（prefix_template 建模）+ spec 响应模型修复

- **做了**：多组 v4/v6 池对绑定（prefix_template 建模 ipv4_cidr↔ipv6_prefix），daemon 按租约 IPv4 最长前缀自动选模板（MatchIPv4Template）；示例 192.168.0.10→2407::192:168:0:10 落地为单测；spec 响应模型缺失修复（15 处响应块重建）+conf/apply 生成接口化。
- **验证结果**：commit 7533703；spec/单测全绿。
- **遗留**：无（本会话与 M4-001 卡片会话2 同步记录）。

## 2026-08-26 · option79 链路落地 + M4-001 日志采集链路

- **option79（用户补充）**：hook 解析 RFC6939 载荷提取 MAC→查 IPv4 池→模板算 IPv6；纯函数+单测落地；§4.2 明确来源优先级链（option79→L2→DUID）。
- **M4-001**：vector.toml 全链路（解析/归一化/CH 批量写）+ logs.sql 建表 + compose 扩展 CH/vector 服务与日志卷；转 review 待环境实测。
- **遗留**：unbound.conf 补 logfile 输出；DHCPv6 事件解析；水位采样 P1。

## 2026-08-26 · M3-006 转 review；M3 代码交付完毕；M4 卡生成

- **做了**：M3-006 五源合成 conf+checkconf 真实现+apply 端点+K1 实证资产落地（转 review）；CI 断言含 K1（local-data 优先 auth-zone）；M4 四张卡入 backlog。
- **里程碑**：**M3 DNS 全量 6/6 代码交付完毕**（§13.4 DNS 六菜单后端全覆盖）。CI 验证异步进行。
- **网络对策**：GitHub 22 端口阻断→SSH over 443（~/.ssh/config 持久化），后续推送稳定。

## 2026-08-26 · M3-005 完成：缓存与安全参数 API

- **做了**：settings 四端点+迁移 0008；参数持久化→渲染→checkconf→reload（校验失败不改运行态）；flush all/zone；每域 TTL 覆盖（F-R3）；Pg/Mem 双仓储。
- **验证结果**：3 新单测全绿；全仓 lint 0 issues。
- **遗留**：checkconf 完整校验 M3-006；TTL 覆盖进 conf 容器实测 M3-006。

## 2026-08-26 · M3-004 完成：封禁管控 API（RPZ 编译管线）

- **做了**：blocklist 五端点+迁移 0007；订阅源同步（拉取/解析/去重/版本递增/失败保旧版）；增量编译（聚合名单→RPZ zone 动作映射→auth_zone_reload）；Pg/Mem 双仓储。
- **验证结果**：5 新单测全绿；全仓 lint 0 issues。
- **遗留**：zonefile 写盘+checkconf 容器实测 M3-006。

## 2026-08-26 · M3-003 完成：解析记录 API（auth-zone/联动视图）

- **做了**：zones/records/linked/export 六端点 spec+迁移 0006；记录类型语法校验；zonefile 导出；变更触发 auth_zone_reload 单区刷新；联动只读视图（绑定→A/AAAA）。
- **验证结果**：4 新单测全绿；全仓 lint 0 issues。
- **遗留**：checkconf→reload 容器实测 M3-006；PTR 联动 P1。

## 2026-08-26 · M3-002 完成：转发规则 API（条件转发）

- **做了**：forward-rules 三端点 spec+迁移 0005；最长后缀优先匹配（默认根域兜底）；dryRun 命令预览；unbound SyncForwardRules；Pg/Mem 双仓储。
- **验证结果**：4 新单测全绿；全仓 lint 0 issues。
- **遗留**：forward_remove 差量 P1；容器实测 M3-006。

## 2026-08-26 · M2-006 完成：前端组织树与地址台账页

- **做了**：TS schema 类型管线接入 gen 流程；类型化 API 客户端；DHCP 一级菜单路由；台账页（组织树+六态着色+保留/绑定操作）；双语 locale。
- **验证结果**：typecheck/build/零外链/Go embed 全绿。
- **里程碑**：**M2 DHCP+IPAM 业务层 6/6 全部交付**（16 端点 API+前端台账页）。

## 2026-08-26 · M2-005 完成：保留与绑定批量 API（事务性全或全不）

- **做了**：bulkReservations 端点；两阶段批量（预检零写入+应用期失败尽力回滚）；ReservationRepo.Delete；handler 复用 LedgerHandler。
- **验证结果**：3 新单测全绿（含整体回滚验证）；全仓 lint 0 issues。
- **里程碑**：M2 API 侧 5/6 完成，仅剩前端页 M2-006。

## 2026-08-26 · M2-004 完成：资产登记 API（MAC 幂等 upsert）

- **做了**：asset 三端点 spec；AssetService（MAC 归一化复用 coherence、幂等 upsert）；Pg/Mem 双仓储；台账 owner 数据源接通（PG asset→ledger Assets）。
- **验证结果**：3 新单测全绿；全仓 lint 0 issues。
- **遗留**：CSV 批量导入归 M2-005。

## 2026-08-26 · M2-003 完成：地址台账 API（六态矩阵+游标+保留/绑定）

- **做了**：ledger 三端点 spec；六态判定全矩阵（§13.4 颜色规范）+v4 逐地址/v6 汇总+游标分页；Reserve/BindStatic 服务（占用检查→预留→Kea 下发）；双预留仓储；KeaDeployer 扩展 reservation-add；main 装配含 PG 绑定源。
- **验证结果**：ledger 4 新单测全绿；全仓 5 包 lint 0 issues。
- **踩坑**：占用判定须查仓储保幂等；LedgerBinding 域内自建类型隔离。
- **遗留**：asset 关联（M2-004）；在线态为租约近似（按决策）。

## 2026-08-26 · M2-002 完成：子网地址池 API + Kea 引擎通道

- **做了**：subnets 四端点 spec（dryRun/KEA_DOWN/双 example）；迁移 0003（subnet/address_pool+org FK）；SubnetService（引擎先发后库/失败不落库/更新回滚）；Pg 与 Mem 双仓储按 IPAM_DB_DSN 装配（顺带落地 M2-001 遗留的 org PG 仓储）；engine/kea 配置生成+ctrl-agent 下发（数组响应语义）；handler 接线。
- **验证结果**：ipam+kea 10 新单测全绿；全仓 lint 0 issues。
- **踩坑**：Kea 响应为数组；spec examples 层级；description 半角冒号。
- **遗留**：compose 真实下发端到端补验（M2-006 后统一）；SUBNET_IN_USE 引用保护待租约表。

## 2026-08-26 · M2-001 完成：组织分组 API（spec 先行全流程首例）

- **做了**：orgs 四端点 spec（树/建/改/删，含 409 ORG_IN_USE·ORG_CYCLE·ORG_NAME_DUP 与 RFC9457 内联结构）；gen 管线升级为 redocly bundle→oapi-codegen；ipam 模块 MemOrgStore+OrgService+OrgHandler；problem 包下沉 internal/pkg 解循环依赖；control-plane 双域 handler 组合。
- **验证结果**：8 单测全绿；build/vet/lint 零问题；闸②路由覆盖率自动覆盖新端点。
- **踩坑**：多文件 $ref 以引用方目录为基准；递归自引用需 bundle 后生成；uuid 参数类型转换。
- **遗留**：pgx 持久化仓储随 M2-002 同批迁移落地；RBAC 中间件 M5 接线（spec 契约已声明）。

## 2026-08-26 · M0/M1 批量验收归档 + M2 任务卡生成

- **做了**：review 列 7 卡（M0×4 + M1×3）批量验收转 done；M0 里程碑正式关闭。生成 M2 六张任务卡入 backlog：组织分组/子网池/地址台账/资产登记/保留绑定五组 API（全部 spec 先行）+ 前端组织树台账页。
- **看板**：done=13 ｜ backlog=6 ｜ review=0
- **下一步**：领取 M2-001 开始业务模块攻坚；§9 真机实测项与 M1-005 遗留（hook 挂载/gRPC C++）待硬件环境并行推进。

## 2026-08-26 · M1-005 验收通过：全栈 7 容器编排贯通，M0/M1 代码交付完毕

- **攻坚过程（7 轮 CI 迭代，诊断经 ci-diag 分支）**：isc 标签不存在→Debian 官方仓自建；NLnetLabs 域不可达→GitHub 源码钉版 1.26.0 自编译+checkconf 门禁；flex/bison/file/unbound 用户四处构建补齐；Kea2.2 output_options 下划线语法；unbound.conf 须 ASCII；APT_MIRROR 环境感知参数。
- **最终断言全绿**：control-plane API ✓ / Kea ctrl-agent version-get ✓ / unbound≥1.16 版本断言 ✓ / 静态权威应答 ✓ / **local_data 动态注入→立即解析（§2.3 热更新链路实证）** ✓。
- **里程碑状态**：M0=100%，M1 代码完成（§9 真机实测项待硬件环境）；review 列 7 卡待批量归档。

## 2026-08-25 · M1-003 验收通过：CI 全绿（含 hook-ci）

- **修复迭代**：hook-ci 首跑 ctest 失败——测试数据 MAC 字段误写为主机名样式被解析器按坏行丢弃；顺藤补齐 Go/C++ 双侧 MAC 归一化对齐与台账写入前防御性归一化。
- **里程碑状态**：M1 进度 75%（M1-005 PoC 真机环境待实施，live-PG/Kea/unbound 实测随之进行）。

## 2026-08-25 · M1-003 完成：薄 C++ hook（核心库+胶水层+行数门禁）

- **做了**：零依赖核心库（MAC 归一化/快照 v2 解析/查找）+ Kea 胶水宏隔离 + ctest + libFuzzer 目标 + 150/800 行数门禁；CI 新增 hook-ci；Go 快照格式同步切 v2 行协议。
- **严谨取舍**：gRPC C++ 客户端移入 M1-005（无头环境不提交臆测实现），当前走 §2.1 快照降级路径。

## 2026-08-25 · M1-004 完成：快照+PG 对账+unbound 下发通道

- **做了**：原子快照（5s 循环）；PG 全量加载+NOTIFY 订阅（断线重连）；§4.4 四 RR 生成与 Reconciler 差分对账（幂等/失败重试语义）；0002 触发器迁移；daemon 三 flag 接线。
- **验证结果**：coherence 包 13 单测全绿；build/vet/lint 零问题。
- **遗留**：live-PG 与 unbound-control 实测在 M1-005 环境；grace 状态机 M2。

## 2026-08-25 · M1 启动：M1-001/M1-002 完成

- **做了**：工具链用户态安装（protoc36.0+gen-go/grpc 钉版）；coherence.proto 契约落地并生成；daemon 核心——B/A 型映射算法（§4.3 样例断言）、ResolveBinding 三态、ReportLease 生命周期、MemStore、UDS 入口。
- **验证结果**：5 单测全绿；build/vet/golangci-lint 0 issues。
- **遗留**：PG 接线与快照(M1-004)、C++ hook(M1-003)、PoC 环境(M1-005)。

## 2026-08-25 · M0-006 验收通过：闸⑥真实 docker 冒烟全绿

- **迭代过程（4 轮红→绿，诊断经 ci-diag 分支匿名可读）**：①CI 无 .env→注入 POSTGRES_PASSWORD；②corepack 签名坑→npm 直装 pnpm+补 .npmrc；③--ignore-scripts 跳过 stub 致 @vben/vite-config 无法解析→显式执行 stub；④容器无 git/不继承 CI 变量致 lefthook prepare 失败→注入 IS_CI/CI。
- **里程碑状态**：**M0 地基 100% 完成**（8/8 卡交付，4 张 review 待批量归档）；M1 五张卡已生成入 backlog。

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
