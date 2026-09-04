# 项目进度日志

> 格式：倒序追加。每次会话收尾必须在此追加一条（对应 AGENTS.md 纪律 3-b），内容=做了什么/改动范围/验证结果/遗留事项。

<!-- 新条目插入到本行下方 -->
## 2026-09-04 · M3-011 补遗⑥——保留与绑定合并单表（状态列+工具栏搜索）

- **合并**：保留列表与静态绑定列表合一（"保留与绑定列表"卡），新增状态列（保留=橙 Tag/静态=蓝 Tag，取自 row.state）；操作列按状态区分（静态=编辑+释放，保留=仅释放）；IPv4/IPv6 RadioGroup 与搜索框置于卡片工具栏一行（左切换右搜索）。
- **部署坑**：docker compose build 静默全缓存（tail -1 空行）导致旧镜像仍在跑——重跑 build 观察 web 阶段真正重执行（167.8s）后恢复；以后部署验证必须看 web 阶段是否重跑而非只看退出码。
- **验证**：Playwright——单卡+状态 Tag 10 行全渲染；搜索 ee:10 命中 1 行且状态列显示"静态"；操作列按状态区分；截图视觉核验通过（placeholder 截断已加宽至 380px）。

## 2026-09-04 · M3-011 补遗⑤——Tabs结构性替换为RadioGroup（根治溢出）+ 组织卡自适应

- **根治**：rc-tabs 对 content-holder 写死测量高度，vxe 异步渲染后溢出卡片（PD 区块漂到框外）——animated/CSS 补丁均无效。结构性替换：子网与保留/绑定页的 antd Tabs 全部改为 RadioGroup 按钮组 + v-if 内容切换（无测量、切换即重挂载、外框天然包裹）；PD 租约去嵌套 Card 改卡内普通区块；v-if 重挂载顺带解决隐藏态 vxe 尺寸失真。
- **组织卡**：144px（原 1/2 宽度再收）+ self-start 自适应高度（不再被 flex 拉伸出大片底部空白）+ 长名称省略号。
- **验证**：Playwright 量测外框 686 ≥ 租约表 661（wrapped:true）、PD 在卡内、截图视觉三查全过（外框包住/无重叠/菜单紧凑自适应）；typecheck/build 绿。


## 2026-09-04 · M3-011 补遗④——组织菜单紧凑化/Tabs重叠修复/新建子网分族（用户三项反馈）

- **组织菜单紧凑**：两页组织卡 256px→208px（w-52），Card body padding 收至 2px，菜单行高 40→32px（13px 字号），子菜单弹出不变形。
- **IPv6 页签重叠**：antd Tabs 默认全页签预渲染，隐藏态 vxe 高度计算失真致 PD 卡与子网表视觉重叠——Tabs 加 destroy-inactive-tab-pane（子网/保留与绑定四处同步加固），切换时重新挂载。
- **新建子网分族**：删除表单中的族选择；"+ 新建 IPv4/IPv6 子网"按钮分别置于各自页签内，openAdd(family) 按页签定族，弹窗标题/字段形态随族自适应。
- **验证**：Playwright——组织卡 208px/行高 32px；v6 页签 PD top 774 vs 表格 bottom 446 无重叠；v6 新建弹窗无族选择且字段为 v6 形态；typecheck 绿。


## 2026-09-04 · M3-011 补遗③——组织垂直菜单/IPv4-IPv6 Tabs/列表模糊搜索/MAC 多格式兼容（用户四项优化）

- **组织菜单**：子网管理与地址台账左侧组织 Tree→antd Menu mode=vertical（子菜单悬停弹出，items 递归生成）；点选过滤、再点取消、保留"全部"按钮。
- **IPv4/IPv6 Tabs**：子网管理的子网表分 v4/v6 两个页签（PD 租约卡归入 IPv6 页签）；保留与绑定两列表各分 v4/v6 页签（同 grid 按 activeTab 切 tableData）。
- **模糊搜索**：保留与绑定页顶部搜索框——IP/MAC/主机名子串匹配（大小写不敏感），命中数实时展示。
- **MAC 多格式**：新增 utils/mac.ts normalizeMacInput（连字符/冒号/点分/裸 12 位/大小写混用，hex=12 校验，与后端 NormalizeMAC 语义对齐）；接入台账绑定/批量导入/编辑改绑三处；后端 bulk 落库改存归一化值。
- **验证**：Playwright——子网页 Tabs+Menu vertical 渲染；搜索 ee:10 精准命中 1 行；C43D-1A07-EB2B 绑定成功且归一化为 c4:3d:1a:07:eb:2b；go test/typecheck 绿。


## 2026-09-03 · M3-011 补遗②——池外绑定不可见根因 + kea毒数据 + 原子性

- **池外根因**：QueryLedger 只为动态池内地址生成台账行——池外手工保留/绑定在静态列表/地图"消失"。修复：池枚举后按子网 CIDR 补发预留/绑定行（去重），列表与地图同步可见。
- **kea 毒数据事故**：测试用非法 MAC（ee:150，末段 3 位）先落库后 kea 下发失败且旧代码无回滚 → 非法 host 进入渲染配置 → 后续所有 config-set 整体被拒。处置：清库 + BindStatic 补 MAC 校验（BAD_MAC 400）+ Reserve/BindStatic/Release/UpdateBinding 全部加"apply 失败回滚"（对齐 bulk 事务语义）+ 迁移 0019（去重 + ipv4 部分唯一索引）+ Upsert 改 ON CONFLICT (ipv4) DO UPDATE（改绑语义落库、不再积累重复行）。
- **颜色**：静态 #69C0FF 与未规划 #9DBEFF 过近 → 静态改深蓝 #2F54EB。
- **多选优化**：转保留多选走事务性 bulk 端点；释放逐地址收集失败明细提示。
- **验证**：池外 .150 绑定→深蓝+列表可见+kea 成功；非法 MAC→BAD_MAC 清晰报错；转保留/释放往返颜色正确；go test/vet 绿。


## 2026-09-03 · M3-011 补遗——204 空 body 解析崩溃修复（用户三项反馈）

- **现象**：转保留/释放报 "Unexpected end of JSON input"；转静态确认后弹窗卡住不关（用户感知"无法绑定确认"）。
- **根因**：api/ipam.ts `req()` 成功分支无条件 `res.json()`——reserve/bind/release/updateBinding 四个端点均返回 204 空 body，解析必抛。绑定流程里 confirmBind 抛错致 vben 模态不关闭且 409 无提示（无 try/catch）。
- **修复**：req() 先 text 后按需 parse（空 body → undefined）；confirmBind 加 try/catch + message.error（409 冲突可见、模态保持可改 MAC 重试）+ 空 MAC warning。
- **验证**：Playwright——转保留/释放成功 toast 无报错；干净绑定 10.99.0.72 模态关闭+变蓝+成功 toast；重复绑定 409 显示"地址已被绑定或在线占用"。


## 2026-09-03 · M3-011 DHCP保留/绑定释放与编辑 + 台账地图优化（用户五项反馈）

- **API**：新增 `POST /ledger/release` + `PUT /ledger/bind`（spec先行+make gen）；`ReservationRepo.UpdateMAC`（PG Upsert 是 ON CONFLICT DO NOTHING 不能复用，改专用 UPDATE）；Service.Release/UpdateBinding + 单测覆盖（释放回归/改绑互转/BAD_MAC/404）。
- **前端**：菜单台账移至子网管理后第二位；保留与绑定两列表加释放/编辑操作列；台账地图「转动态」改「释放」（逐地址调 API 后刷新，颜色实时回归）；修弹窗铺满全屏根因——`width="540"` 字符串是无效 CSS，改数字绑定并瘦身表单（删假编辑的租约字段）。
- **坑位**：PG Upsert ON CONFLICT DO NOTHING 会静默吞掉改绑；antd 中文按钮自动插空格（"编 辑"），Playwright hasText 须用 `\s*` 正则；vxe 操作列按钮在行内但 Playwright 对主行副本点击超时需注意。
- **验证**：Playwright 全流程——菜单顺序/转保留与释放颜色实时变化(#FF9C6E↔#9DBEFF)/绑定409占用检查/编辑MAC(PUT 204→ee:99)/释放(POST 204→行消失→地图回归灰蓝)/编辑弹窗520×626；go build+vet+test 绿、web typecheck 绿。


## 2026-09-03 · M3-010 补遗——answer_ip 前端断链修复（toGenLogRow + CSV 导出）

- **现象**：CH 有 answer_ip 但日志页「应答IP」列全空——API 层 `toGenLogRow` 漏映射 AnswerIp；CSV 导出亦缺该列（表头 11 列与 SELECT 12 列错位）。
- **修复**：handler.go 两处补齐（JSON 映射 + CSV 表头/行）；go build/vet/test 绿。
- **验证**：Playwright 实测 resolve 事件 21/22 展示应答IP（NXDOMAIN 无 IP 符合预期）。


## 2026-09-03 · M3-010 unbound python 模块——DNS 解析 IP（answer_ip）落地

- **根因**：unbound 1.26.0 log-replies 行不含应答区（verbosity≤4 实测均无），answer_ip 永远为空。
- **方案**：编译 `--with-pythonmodule`，dns_log.py 在 MODDONE 提取应答区（A/AAAA/CNAME 链），输出 DNS_LOG JSON 顶层含 answer_ip；vector 新增 parse_dns_log 消费；log-replies 关闭去重；SERVFAIL 补记。
- **坑位**：jammy 需 PYTHON_VERSION=3.10；pythonmod 必须 inform_super 函数 + init(id,cfg) 双参；选项名 python-script；VRL 0.46 无 for 循环/to_timestamp（选择逻辑放 Python 侧）；vector/unbound 配置均打入镜像，改配置须重建而非 restart。
- **验证**：递归/缓存命中/NXDOMAIN/local-data 四类行为符合设计，CH 无重复事件，answer_ip 正确入库（IPv4 以 ::ffff: 映射存储）。
- **文档**：架构文档 §6 已同步决策与已知取舍；任务卡 M3-010 归档 done。


## 2026-09-01 · vxe 客户端分页修复——13 条记录正确分 2 页

- **问题**：转发规则 13 条记录 10/页却全在第 1 页、第 2 页空——vxe 仅代理模式自动切片，tableData（客户端数据）不切片。
- **修复**（共享 use-vxe-grid.vue 全局生效）：① 客户端数据手动切片（slice((page-1)*size, page*size)）；② 同步 pagerConfig.total/currentPage/pageSize；③ 绑定 `page-change` 事件（注意：vxe 事件名是 page-change 非 pager-change）更新页码状态。
- **部署教训**：docker 构建的 web 阶段曾用缓存层导致旧 bundle 一直运行——本次确认 web 阶段重跑（COPY web/packages 失效）+ 验证服务端 chunk 含 pageChange。
- **验证**：forward 第1页 10 行/第2页 3 行/active 2；upstream 10+1 两页；subnets 7 行单页；pager 总数全部正确。


## 2026-09-01 · vxe 列自适应优化 + users/roles/仪表盘间距

- **vxe 列**：非操作列 width→minWidth（内容列自适应填满容器），状态类列保持固定宽（协议/族/状态等），操作列 `fixed: 'right'` 贴右缘——实测无右侧空列，列宽均衡铺满。
- **间距**：users 页补 p-4（roles 已有）；仪表盘 grid 容器加 mb-4（服务健康/DNS 服务 与 DNS QPS 卡间距 35px）。
- **验证**：Playwright 视觉确认——upstream 操作列贴右无空隙、各列填满；dashboard 卡间距正常；users p-4 生效。


## 2026-09-01 · vxe 表格高度修复——全部行可见不滚动 + pager 总数同步

- **问题**：vxe 表格只显示 ~2 行，需上下滚动（adapter 全局 minHeight:180 把 grid 锁高，配合 height:'auto' 只渲染两行高度）。
- **修复**：① 移除所有 grid 的 `height: 'auto'`（vxe 默认内容高度）；② adapter `minHeight: 180→0`；③ 共享 use-vxe-grid.vue：tableData（客户端数据）时同步 `pagerConfig.total`（分页"共 N 条"正确显示）。
- **效果**：表格按当前页行数撑开——默认 10 行、不足按实际（7/11/13 行全展示）、选 20 条/页显示 20 行，无内部滚动。
- **验证**：Playwright 实测 upstream 11 行 innerH 440（11×40）、pager 共 11 条；forward 13 行、共 13 条。


## 2026-09-01 · 仪表盘 QPS 折线 + 全站间距 + vxe 默认10行 + 日志分页真修复

- **仪表盘**：新增「DNS QPS 折线」卡片（SVG polyline 零依赖，60s 分桶近 1 小时）；地址池利用率 TopN 加 mt-4 与上方间距。
- **间距**：orgs/roles/settings/tail 单 Card 页加 p-4 包装（对齐 ledger）。
- **vxe 表格**：adapter 默认 pageSize 20→10（每页最少 10 行，选 20 显示 20 行，height auto 全展示无内滚动）。
- **日志分页（真修复）**：此前「修复」注释错误——SQL `LIMIT ? OFFSET ?` 但 args 顺序仍是 [offset, limit]（LIMIT 拿 offset 值）→ page≥3 越取越多。真正确认：limit 在前、offset 在后。实测 page 1/2/3/5 全 50 条、page 77 空（正确越界）；UI 点击第 3 页显示 50 行、total 3392。


## 2026-09-01 · 全站表格 VbenVxeGrid 化（9 页 15 表）——固定操作列 + 水平按钮

- **转换**：upstream / forward / options(2) / dualstack / reservations(2) / users / roles / records(2) / blocklist(3) 全部 antd Table 换 VbenVxeGrid（useVbenVxeGrid + :table-data 响应式 + rowConfig.keyField）。
- **固定操作列**：op 列 fixed:'right'（subnets/forward 实测激活 pinned；溢出时自动固定）。
- **水平按钮**：编辑/删除/条目/同步/编译 按钮包 flex 行。
- **踩坑**：onMounted 正则插入破坏语法（两次修复）；pagerConfig:false 类型不合法→不设置；列宽调大激活 fixed。
- **验证**：typecheck 0 + build ✓ + Playwright 各页 vxe 渲染/数据行正常。


## 2026-09-01 · 子网页 vxe 化 + 全站模块间距（对齐地址台账）

- **子网页**：① 表格换 VbenVxeGrid（`useVbenVxeGrid` + `:table-data` 响应式数据 + 操作列 `fixed: right`）；② 组织树切换修复（实测点「总公司」过滤生效——此前为中断部署空表假象）；③ 编辑/删除按钮水平排列。
- **间距举一反三**（对齐 ledger `p-4` 模式）：reservations/options/cache/security 的 grid 容器加 `p-4`；dualstack/upstream/forward/blocklist 单 Card 页加 `p-4` 包装；records 根 div 加 `p-4`。
- **踩坑**：① vxe `gridOptions.data` 非响应式（捕获初始空数组）→ 改 `:table-data` prop；② Tailwind `m-4` 类被 antd `.ant-card` 规则覆盖不生效 → 统一用 `p-4` 包装 div。
- **验证**：subnets 实测总公司切换→表格 7 行、编辑/删除水平、vxe 渲染；各页 parentPad 16px。

## 2026-09-01 · 地址地图样式修复——还原 ip-plan-map/index.html 视觉效果（格子/间距/颜色/图例/标题）

- **问题**：格子挤在一起无空隙、尺寸缩小（18×16px）、颜色不突出、图例色块消失。
- **根因（实证）**：Vue `:style` 对象里的**数值型长度属性**（width/height/fontSize/borderRadius/gap/maxHeight/paddingBottom）在运行时被 style helper 丢弃（`c()` 序列化怪癖）——格子只剩文字大小、无 gap、图例色块 0px。
- **修复**：静态样式（尺寸/间距/圆角/字体）改 **scoped CSS 类**（`.ip-grid`/`.ip-cell`），动态颜色/边框/透明度留内联；图例色块与叠加圆点的数值改**字符串 px**；地图卡片 `max-width: 980px` 还原参考容器；标题去掉 `:title` prop 只留 slot（cidr 蓝色 Tag 正常显示）。
- **验证（Playwright 实测）**：格子 32×32px + gap 4px + max-height 420px；图例色块 12px；卡片 980px（每行 26 格，与参考 25 接近）；标题含「地址规划」+ 蓝色 cidr Tag；广播/网络格 #637196 灰色正确。

## 2026-09-01 · 地址地图布局还原——还原 ip-plan-map 原始样式（去拥挤）

- **问题**：移植后地图被嵌套 Card + 挤在组织树旁，拥挤不美观。
- **修复**：对照 index.html/demo 原始布局——① 去掉外层「地址台账」Card（IpPlanMap 本身是 Card，独立展示）；② 页面加 `p-4` 留白；③ 组织树收窄 260px；④ IPv6 网段表格独立 Card 置底（mt-4）；⑤ 移除冗余「IPv4 地址使用情况」头部（地图 Card 自带「地址规划」标题 + cidr Tag）。
- **验证**：typecheck 0 + build ✓ + 容器重建 + ledger chunk 含 p-4/mt-4/IPv6 网段标记。

## 2026-09-01 · 地址台账优化——组织→网段 v4/v6 分栏 + IPv4 地址地图（移植 IpPlanMap）

- **需求**：组织分组下对应各组织的网段，分 IPv4/IPv6；IPv4 使用情况用 tmp/ip-plan-map 组件。
- **做了**：① 后端 `GET /ledger` 加 `subnetId` 过滤（spec+gen+handler+mem 过滤）——按子网拉逐地址台账；② 移植 `IpPlanMap.jsx`（React+antd5）→ `web/apps/web-ipam/src/components/ip-plan-map.vue`（Vue3+antdv）：地址地图格子/状态着色/叠加角标/图例/统计/点击与 Shift 区间选择/批量操作/编辑弹窗/详情抽屉/网段切换，图标用 lucide；③ 重写 ledger 页：左组织树，右 v4/v6 分栏——IPv4 子网下拉 + 地址地图（台账状态映射：online→dynamic+在线角标、grace→dynamic、conflict→dynamic+冲突角标、available/static/reserved 直通），IPv6 网段表格（PD/地址池摘要）。
- **操作接线**：转保留→reserveAddress、转静态→BindModal 输 MAC、转动态→提示 Kea 自动管理。
- **踩坑**：移植时修正 React 原组件 bug（hostToIp 按 host 键却用 ip 查询→selectedIps 恒空，改 ip 键 map）；Vue SFC 不能 export interface / JSX 渲染函数；antdv Space size 需数字；Select change 签名包装。
- **验证**：typecheck 0 + build ✓ + 容器重建 + 后端 subnetId 过滤实测（子网 91 行，states available/reserved/static）；ledger chunk 含地址地图标记。

## 2026-09-01 · 日志明细分页修复——offset 分页接通（页码翻页 + 每页条数调整）

- **现象**：日志明细分页条是装饰性——游标式后端未接页码点击，无法翻页/调每页条数。
- **修复**：后端加 `page` 参数（offset=(page-1)*pageSize；LogPageNo 参数避免与响应 schema 撞名），ch_store/mem_store/service/handler 全链路；前端 page/pageSize 状态 + antd 分页条 `current/total/onChange` 接通。
- **验证**：后端 page1/page2（pageSize=5）各返回 5 条、total 一致；typecheck 0 + build ✓ + 容器重建。

## 2026-09-01 · 修复：QPS 曲线数据骤减——normalize VRL 语法错误致摄入停摆

- **现象**：查询检索 QPS 曲线数据大幅减少。
- **根因**：① normalize 里 `"rcode": if .rcode == null {""} else {.rcode}`——**VRL 的 if 是语句不是表达式**，对象字面量里用 if 表达式=语法错误 → vector 拓扑编译失败 → **全部摄入停摆**；② 早前宽松正则把 verbosity≥2 的 `resolving` 模块行误计为查询，基线被抬高。
- **修复**：normalize 改为**语句先行**（rcode_v/answer_v 变量赋值后再进对象字面量）；DHCP 行 rcode 强制落空串（CH 非空列）。
- **验证**：vector 0 语法错误；新查询成对入库（dns_query + resolve/NOERROR）；QPS 接口正常返回近期分钟级真实流量。

## 2026-09-01 · DNS 日志应答解析（方案 A 落地 + unbound 限制实证）

- **做了**：① unbound `log-replies: yes`；② vector 应答行正则 → **rcode 激活**（NOERROR/NXDOMAIN 入库，前端「应答码」列不再是空）+ answer_ip 字段；③ CH `logs` 加 `answer_ip Nullable(IPv6)`（logs.sql + 运行实例 ALTER）；④ spec/gen/后端（LogRow.answerIp、answerIp 过滤参数、ch_store 读写、MemStore 匹配、handler 接线）；⑤ 前端「应答IP」列 + 按应答 IP 过滤输入框（反查域名）。
- **实证结论（重要）**：unbound 1.26.0 的 `log-replies` 应答行在 **verbosity 1-4 均不含应答区**（实测 `... IN RCODE time rtt qtime` 即止）——`answer_ip` 目前诚实置 null，待 **unbound python 模块**（`--with-pythonmodule` + operate 钩子取 return_msg 应答）等数据源接入后填充；rcode 解析已完整可用。
- **踩坑**：① VRL 数组下标仅允许字面量（`matches[n-1]` 语法错→改尾部锚定正则）；② 宽松 IPv6 正则把 qtime 纯数字误判为 IP、且 verbosity≥2 的 `info: resolving X A IN` 模块行被 query 正则误匹配（srcip="resolving" 塞进 sip 致 CH 400）→ 收紧 srcip 必须为 IP、IPv6 需含冒号；③ vector 磁盘缓冲残留旧配置脏事件→清 data_dir。
- **验证**：vector 0 报错；CH resolve 行 rcode 分布 NOERROR/NXDOMAIN 正常、sip 为合法 IP、answer_ip 为 null；API `answerIp` 参数 200；前端 chunk 含「应答IP」列。

## 2026-09-01 · 三项优化：图标裁剪上传 / ant-card 基础外边距变量 / 日志自定义时间区间

- **① 系统设置站点图标**：原生 input 换 adapter 增强 Upload（`crop="true"` + `aspect-ratio="1:1"` + `max-size=2MB` + `picture-card`）——选图自动弹 VCropper 1:1 裁剪，`custom-request` 收 1:1 Blob → base64 双写 faviconUrl/logoUrl。
- **② ant-card 外边距**：改为 `:root{--card-margin-base:12px}` 基础变量，`.ant-card{margin-bottom:var(--card-margin-base)}`（flex/grid 直子排除）——全站可统一微调。
- **③ 日志检索时间区间**：预设 Select 旁加 `DatePicker.RangePicker`（showTime HH:mm:ss，格式 YYYY-MM-DD HH:mm:ss）——自定义起止日期时间查询，优先于预设；loadLogs/loadAgg/导出统一走 `timeWindow()`。
- **验证**：typecheck 0 + build ✓ + 容器重建 + settings chunk 含 aspect-ratio/picture-card、logs chunk 含 RangePicker、bootstrap css 含 --card-margin-base；settings/logs API 200。

## 2026-09-01 · 全局 ant-card 外边距

- **做了**：`@vben/styles/antd/index.css` 全局规则——`.ant-card{margin-bottom:12px}`，`.flex>/.grid>` 直子排除（横向布局由 gap 承担不叠加）；ledger 内联 display:flex 容器改 `flex h-full gap-4` class 纳入排除。
- **验证**：typecheck 0 + build ✓ + 容器重建 + 服务端 bootstrap css 实测含 `ant-card{margin-bottom:12px}` 与 `.flex>.ant-card,.grid>.ant-card{margin-bottom:0}`。

## 2026-09-01 · 子网管理页布局：子网与地址池 + PD 租约改上下结构

- **做了**：原三栏横排（组织树|子网|PD租约）改为：组织树固定左栏，右侧 `min-w-0 flex-1` 竖排容器——子网与地址池在上、PD 租约在下（mt-3）。
- **验证**：typecheck 0 + build ✓ + 容器重建 + chunk 含新容器标记；subnets API 200。
- **遗留（部署侧）**：`/dhcp/leases6` 当前 500——Kea dhcp6 命令通道不可达（lease6-get-all 转发失败，server offline），需核查 kea-dhcp6.conf 的 control-socket/ctrl-agent 转发配置。

## 2026-08-31 · M2-041 补4：组织树 UI 优化

- **做了**：工具栏（总数 Tag + 展开/收起全部）、节点图标（building-2/叶子箭头）、子节点数徽标、show-line 连接线、圆角 hover/选中态、受控 expandedKeys。
- **验证**：typecheck 0 + build ✓ + 容器重建 + chunk 标记确认。

## 2026-08-31 · M2-041 补3：创建成功但报「未知 API 路由」——listOrgTree 调错端点（/orgs/tree 不存在）

- **根因**：spec 树端点 `GET /orgs`，前端 listOrgTree 却调 `/orgs/tree`（404，M2-001 起就错）→ 组织树从未加载，创建后 load() 抛错。
- **修复**：listOrgTree → `/orgs`；Go 注释同步。
- **验证**：create 201 + GET /orgs 树含新根；部署 chunk 无 /orgs/tree。

## 2026-08-31 · M2-041 补2：orgs 最终根因——漏 import Input（v-model 断链）+ UI 优化

- **现象**：已输入仍提示「请输入组织名称」→ confirmName 调用但 nameInput 空。
- **根因**：orgs 页 antd import 漏 `Input`，无全局注册 → 渲染原生 `<input>`（不触发 update:value）→ v-model 断链；ledger 页同类。
- **修复**：orgs/ledger 补 Input import；orgs 输入框加 label+autofocus+加宽。
- **验证**：typecheck 0 + build ✓ + 新 chunk 含 onUpdate:value 绑定。

## 2026-08-31 · M2-041 补：orgs 三次报障真根因——静态资源无缓存头

- **现象**：orgs 新建根组织第三次报障；后端 POST /orgs=201 正常、部署产物代码正确。
- **真根因**：serveStatic 无任何缓存头 → 浏览器启发式缓存旧 index.html → 持续加载旧 chunk，部署修复不生效。
- **修复**：index.html no-cache + 哈希资产 immutable 长缓存；orgs 空名警告。
- **验证**：缓存头 curl 实测正确；新 chunk 含修复标记；POST 201。用户需强刷一次后永久生效。

## 2026-08-31 · M2-041 全站 UI 批修（8 项清单 + 举一反三）

- **修**：①orgs 新建根组织（Modal 流程重写）②角色管理跳系统设置（路由缺 name → finalRoutesMap[undefined] 键碰撞，补 name）③设置页同步侧栏（updatePreferences）④端口可编辑+重启接口（迁移 0018 + POST /system/restart + listen 读 DB）⑤上游 Modal 不弹（async close 竞态 → openAdd）⑥转发编辑重复取消/缺字段（清理+label）⑦转发启用/删除（Switch toggle + 错误处理）⑧封禁条目迁 VbenDrawer + 客户端分页。
- **举一反三**：dualstack 同型竞态修复；ledger/records/options/roles body 按钮残留清理；roles VbenModal v-model:open 转命令式；无 name 路由全站审计。
- **验证**：typecheck 0 + build ✓ + 零外链 PASS + go build ./... + 单测 OK + 冒烟全 200 + restart 204 容器恢复。

## 2026-08-31 · M2-045 完成：站点图标共用 + 本地上传

- **做了**：设置页 favicon/侧栏 logo 两输入框合并为本地图片上传（dataURL 双写 + 预览/清除 + 200KB 限制）；spec maxLength 放宽至 200000 支持 base64 存储。
- **验证**：typecheck 0 + build ✓ + 零外链 PASS + 容器重建 + e2e（PUT 13.6KB dataURL 200 / GET 回读完整）。

## 2026-08-31 · M2-042 完成：封禁管控左右布局 + 可收缩

- **做了**：名单列表与条目列表左右分栏（左 46% 可收缩 + 右 flex-1），extra 收缩 toggle，空态引导提示。
- **验证**：typecheck 0 + build ✓ + 零外链 PASS + 容器重建 + 冒烟 200。

## 2026-08-31 · M2-044 完成：Modal 确认按钮文案化（全站 12 Modal）

- **做了**：全站 12 个 Modal 的 footer 确认按钮改为操作名（如「添加」「创建（下发 Kea）」「绑定」），onConfirm 承接原 body 按钮逻辑，body 内操作按钮全部移除；编辑场景动态「保存修改」。
- **验证**：typecheck 0 + build ✓ + 零外链 PASS + 容器重建 + 冒烟 200。

## 2026-08-31 · M2-043 完成：二级菜单全站空白修复（多根页面级联）

- **根因**：vben v5 路由切换动画撞多根节点组件即级联损坏全部后续导航；M2-037 批 4 将 3 个 Modal 挂到 template 顶层致 orgs/records/ledger 多根。
- **修复**：三页 template 包 `<div>`；typecheck 0 + build ✓ + 零外链 PASS + 容器重建 + 冒烟 200。
- **关联**：M2-041 四按钮无反应大概率同根因（页面未渲染），随本卡修复，待用户复核。

## 2026-08-31 · M2-040 完成：黑名单封禁不生效（编译闭环自动化）

- **根因**：M2-033 的封禁是运行时 local_zone static 注入，编译需手动触发（UI 编译按钮），用户加条目后未点编译 → 运行时态空 → 解析仍通。
- **修复**：① 条目/策略组/同步变更后自动 ReplayAll（加完即生效）；② 删除链路改 local_zone_remove（ReplayAll 只加不减，残留导致删了还拦）。
- **验证**：e2e 闭环全过——启动重放 NXDOMAIN / 基线可解析 / 加条目自动拦 / 删条目自动通；用户条目 www.crphb.com.cn 已拦。

## 2026-08-31 · M2-039 完成：系统设置二级菜单（批 1 后端 + 批 2 前端）

- **批 1**：GET/PUT /system/webui-settings + webui_settings 单行表（迁移 0017）+ WebuiHandler（serverIp/serverPort 从 Host 只读解析）——e2e 四步全过。
- **批 2**：settings/index.vue 设置页（siteName/faviconUrl/logoUrl 输入 + serverIp/Port 只读展示 + document.title/favicon 动态应用）+ system.ts「系统设置」二级菜单路由。
- **验证**：typecheck 0 + build ✓ + 零外链 PASS + 容器重建 + webui-settings API 冒烟 200（siteName 持久化复核）。
- **v1 决策**：logo 上传以 URL 输入实现（文件上传 v2）；服务器 IP/端口只读展示（Web 改部署配置是反模式——改完失联）。

## 2026-08-31 · M2-039 批 1：系统设置二级菜单（后端 webui-settings）

- **做了**：spec 三处 + gen + 迁移 0017（webui_settings 单行表）+ webui_handler.go（GET/PUT + serverIp/serverPort 从 Host 只读解析——修改走部署配置，Web 改部署配置是反模式）+ main/handler_test 装配。
- **验证**：BUILD_OK + go test 无失败 + lint 0 issues + 容器重建 + e2e 四步全过（GET/PUT/持久化复核/缺 siteName 400）。
- **踩坑留痕**：首次实施全部丢失——每步同批验证（ls/grep）重建实锤；upsert 的 ON CONFLICT 参数复用必须用 EXCLUDED。
- **遗留**：批 2 前端（设置页 + 动态应用）+ 批 3 收尾。

## 2026-08-31 · M2-038 完成：日志分页优化（分页移左 + 每页 20/50/100/200）

- **做了**：logs/search Table 分页配置——position bottomLeft（分页移左，返回顶部按钮避让）+ pageSizeOptions 20/50/100/200 + showSizeChanger + showTotal「共 N 条」。
- **验证**：typecheck 0 + build ✓ 7.60s + sync/offline PASS + 容器重建 + logs API 冒烟 200（from 必填参数修正后复核）。

## 2026-08-31 · M2-037 完成：Modal 不弹出修复（全站 10 页 13 Modal useVbenModal 化收口）

- **四批收口**：批 1 subnets 试点（a582254）→ 批 2 upstream/forward（e42c1da）→ 批 3 dualstack/options/users/blocklist（04aa5ce）→ 批 4 orgs/records/ledger（本批）。
- **根因沉淀**：vben v5 的 Modal 显隐是 modalApi.open()/close()/setState() 命令式，v-model:open 与布尔 ref 是错误用法（ModalProps 无 open 字段）。
- **验证**：各批 typecheck/build/零外链全绿 + lint 0 + 容器重建 + 11 端点冒烟全 200；全部页面 Modal 可弹出，v-model:open 清零。

## 2026-08-31 · M2-037 批 3：Modal 修复（dualstack/blocklist/users/options）

- **做了**：四页 useVbenModal 命令式改造（dualstack 7 处 + blocklist 6 处 + users 6 处 + options 10 处——双 Modal 实例/闭标签配对/按钮 setState+open）。
- **验证**：typecheck 0 + build ✓ 6.53s + sync/offline PASS + showOptForm/showClsForm 残留清零。
- **踩坑留痕**：开闭标签必须成对替换（OptModal/ClsModal 开后 </VbenModal> 未配对致 Invalid end tag）；process 批量改造中途失败不落盘——分文件独立落盘。
- **批 4**：orgs/records/ledger 三页后续。

## 2026-08-31 · M2-037 批 2：Modal 不弹出修复（upstream + forward）

- **做了**：两页 useVbenModal 命令式改造（imports/Modal 实例/add 尾 close/edit 内 setState+open/cancelEdit 内 close/模板 FormModal/按钮 setState+open——upstream 8 处 + forward 7 处 + 取消按钮内联 showForm 修正）。
- **验证**：typecheck 0 + build ✓ 6.61s + sync/offline PASS + Go 全链绿 + 容器重建 + 上游/转发 API 冒烟 200。

## 2026-08-31 · M2-037 批 1：Modal 不弹出修复（subnets useVbenModal 试点）

- **现象**（用户调试）：点击新建子网/添加上游等按钮无 Modal 弹出——M2-034 的 v-model:open 全部无效。
- **根因**：vben v5 的 Modal 显示控制是 modalApi.open()/close()/setState() 命令式（modal-api.ts:97/158/171 实锤），ModalProps 无 open 字段——v-model:open 为错误用法。
- **修复**：subnets 页 useVbenModal 连接组件模式改造 7 处（const [FormModal, formModalApi] = useVbenModal；open/setState/close 替代 showForm 状态）。
- **验证**：typecheck 0 + build ✓ 7.59s + sync 4.3M + 零外链 PASS。
- **举一反三**：M2-034 改造的全部页面（9 页）同款修复——批 2/3/4 进行中。

## 2026-08-31 · 批 3 修正：二级菜单消失 bug（用户调试发现）

- **现象**：M2-035 批 3 的 meta.authority（权限点）上线后二级菜单全消失——admin 也看不到。
- **根因**：vben 底座的菜单过滤匹配 guard 传入的 access=userInfo.roles（角色名），与 authority=[权限点] 无交集。
- **修复**：guard.ts:96 userRoles 改混合数组（[...roles, ...permissions]）——角色名与权限点混合匹配。
- **验证**：typecheck 0 + build ✓ + sync/offline PASS + 容器重建——用户刷新确认恢复。

## 2026-08-31 · M2-035 完成：RBAC 角色管理（四批全收口）

- **批 1**：权限点模型（6 域×读写=12 点）+ 迁移 0016（roles 表+四内置种子）+ RBAC 中间件重构（domainOf/builtinRolePerms/hasPerm——域权限拦截替换 admin 硬编码）+ 域拦截 403 决定性（8fd14b5）。
- **批 2a**：roles API 后端闭环（spec/gen/role_store/roles_handler/main 装配 + 自定义角色绑定 e2e 决定性——dhcp-viewer 用户 GET /subnets 200 / GET /users 403）（452a5fc）。
- **批 2b**：前端角色管理页（角色列表 Table + 权限矩阵 VbenModal + builtin 保护 + 路由注册）（841fc79）。
- **批 3**：前端菜单过滤（UserInfo 加 permissions/hasPerm 逐点过滤/ListAuthCodes 改权限点/accessCodes 接线/meta.authority 批量）——/auth/codes 12 点全实收（本批）。
- **里程碑**：**用户清单五-3/5-4 闭环**——角色分为管理员/操作员/审计员/自定义；角色管理可自定义/编辑/按菜单授权；**不只隐藏菜单——接口同样拦截**（403 决定性验证）。

## 2026-08-31 · M2-035 批 2a 完成：roles API 后端闭环

- **做了**：spec（/roles CRUD 四操作）+ gen；role_store.go（RoleStore/Mem/Pg + RegisterRole 联动 normalizeRoles 白名单）；roles_handler.go（四方法 + builtin 409 + validatePerms）；main.go 装配（full 结构 + 启动加载自定义角色注册）。
- **验证**：e2e 决定性——内置 4 角色实收（admin 12 权限）；自定义角色 dhcp-viewer 绑定用户后 GET /subnets 200 / GET /users 403（权限点生效）；builtin 409 保护。
- **踩坑留痕**：gen 的 enum 字段生成独立类型需 string 转换；roleId 主键是 name（spec 不能写 format: uuid）；handler_test 在包内引用不需要包名前缀。

## 2026-08-31 · M2-035 批 1 完成：RBAC 域权限模型（权限点/迁移 0016/中间件重构）

- **做了**：迁移 0016（roles 表+四内置角色种子：admin/operator/auditor/user）；rbac.go 重构（domainOf 路径→域映射/builtinRolePerms 内置角色权限集/hasPerm 逐角色解析——内置映射优先+permLookup 查库/NewRBACMiddleware 加 permLookup 参数/域权限拦截替换 admin 硬编码）；main.go permLookup 闭包（roles 表查询）。
- **验证**：迁移 0016 四内置种子实收；admin 全通（dash/subnets/users 200）；operator 权限集生效（system:read 200/dns:write 201）；**域权限 403 决定性**（op POST /users → 403 需要 system:write）；全量 go test 绿 + lint 0。
- **踩坑留痕**：NewRBACMiddleware 子串替换把 permLookup 插进了 r.Use(platform. 与调用之间（前缀在 m.group(0) 外）——子串替换必须看前缀上下文；permLookup 的 json.Unmarshal 需 main 补 encoding/json import。
- **批 2/3**：roles API + 前端角色管理页/菜单过滤——后续。

## 2026-08-31 · M2-034 完成：全站表单 VbenModal 化（六批）

- **做了**：subnets 试点（批 1）→ 举一反三九页（批 2a upstream+forward / 2b dualstack / 2c options 两表单 / 2d-1 users+blocklist 名单创建 / 2d-2 orgs+records+ledger 的 prompt 改造）——全部页面表单/交互统一 VbenModal（可拖拽/自动高度，@vben/common-ui）。
- **验证结果**：各批 typecheck/build/零外链/容器冒烟全绿。
- **踩坑沉淀**：包裹式替换数清闭合标签；各页缩进/状态函数形态先探再改；python 中途失败不落盘——重跑前 grep 各页状态。

## 2026-08-31 · M2-034 批 1：全站表单 VbenModal 化（subnets 试点）

- **做了**：dhcp/subnets 页 VbenModal 化（@vben/common-ui 的 VbenModal——draggable/title/自动高度内置；表单区块 → Modal 包裹；cancelEdit 函数新增；新建按钮 Modal 语义）。
- **验证结果**：typecheck 0 + build ✓ 6.67s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ 冒烟 200。
- **踩坑留痕**：包裹式替换必须数清闭合标签（内层 </div> 被 </VbenModal> 覆盖致 build 失败）；Modal 化前先确认页面状态函数清单（cancelEdit 不存在需新建）。

## 2026-08-29 · M2-036 完成：日志中心优化与 P0 修复

- **修复**：tail SSE 重写为 fetch 流（EventSource 无法携带 Authorization 的 401 重连循环根除）；解析记录校验提示（静默 return 根除）；search 页加应答码/应答服务器列；cache 页加性能统计区（1h/24h/峰值，getLogQps 消费）；黑名单封禁闭环（local_zone 方案，45a028f）。
- **验证**：SSE 流 4 帧真实数据；冒烟全 200；黑名单双向 e2e（拦截→恢复）；全链绿。
- **移交**：window.prompt modal 化归 M2-034；RBAC 角色管理归 M2-035。

## 2026-08-29 · 黑名单封禁闭环打通（local_zone 方案，用户调试发现 RPZ 不生效）

- **现象**：封禁条目 www.crphz.org 编译返回成功但域名仍可解析——假阳性（crphz.org 公网本就不存在，NXDOMAIN 是真实递归结果）。
- **根因三重**：Compile `_ = text` 从未写 zonefile（M2-027 未完成实现）+ unbound.conf 无 auth-zone 段 + reload 对不存在的 zone 无意义；RPZ 修复尝试确认容器 unbound 1.26 编译下 auth-zone 内 rpz: yes 不可用。
- **方案**：改 local_zone static 拦截（unbound 标准能力）——Compile 下发 local_zone 命令 + ReplayAll 启动重放 + 失败即上报。
- **双向 e2e 决定性**：加条目+compile → NXDOMAIN；删条目+local_zone_remove → 恢复可解析。
- **遗留**：删除对称（local_zone_remove 自动触发）后续卡。

## 2026-08-29 · dnsmasq 配置迁移验证（用户调试）

- **迁移**：上游 7 条（114/v6×2/内网×4）+ 转发规则 12 条（8 泛域 + 3 精确例外 + cpms.szecp）经 API 迁移，前端上游管理/转发规则页可见。
- **验证**：`unbound-control list_forwards` 实收 12 条 zone（含根 "." 挂全部 enabled 上游，v6 forward-addr 格式正确）；dig 实测——runwork-h5.crc.com.cn（精确例外→114）NOERROR、test.crc.com.cn（crc.com.cn→10.61.0.136 内网）NXDOMAIN 真实响应。
- **架构认知修正**：forward 下发是双轨——BuildConf 渲染快照 + `unbound-control forward_add` 运行时增量（svc.Create/Update/Delete 内部各自执行，SyncForwardRules 触发链完整；service.sync() 为死代码但非必需）。
- **遗留**：运行时增量不回写渲染 conf——**unbound 重启后 conf 快照过期**（需规则回写渲染或 daemon 周期重 sync，后续卡）；bogus-priv/domain-needed/cache-size 为 BuildConf conf 层增强项（P3）。

## 2026-08-29 · fix：dashboard poolTop v6 池 panic（用户调试发现）

- **现象**：dashboard 四灯全部「未接入」——GET /dashboard 500 空 body（gin recovery）。
- **根因**：M2-018 的 v6 池 → poolTop → DistinctClientIP(rng=v6 范围) → mappedV6(纯 v6) 调 As4() panic（netip 约束：As4 仅对 v4/mapped 合法）。
- **修复**：mappedV6 分支化（v4→::ffff mapped；纯 v6→原样 16 字节，与 CH IPv6 列存储一致）+ cidr_test.go 回归锁（新建）。
- **验证**：全量 go test 绿 + lint 0；容器重建后 GET /dashboard 200。

## 2026-08-29 · M2-033 完成：Option 79 消费链路（hwaddr 投影）

- **做了**：spec hwAddress/hwAddrSource + gen；Lease6 投影补 hw 三字段；lease6Fn 条件投影；前端 PD 租约卡 MAC 列。
- **验证结果**：全链绿；e2e——lease6-add 带 hw-address → agent 原始响应含 hw-address → API 回读 MAC 全链一致；hwtype/hwaddr-source add 场景 Kea 不持久化（真实 relay 报文由 Kea 内部推导，投影代码同构就绪）。
- **交付要求**：核心 DHCPv6 relay 启用 RFC 6939（网络设备侧配置）。
- **里程碑**：地址族关联方案（Option 79 路径）**消费侧闭环**，MAC 数据就绪待 NDP/AC 采集器配合。

## 2026-08-29 · M2-032 完成：质量与健壮性收尾

- **做了**：回归锁单测 ×3（kea result-3 容忍 / blocklist 删除级联+自然键 / MatchPolicyView 五分支，抽包级函数）；sentinel 统一（双定义清理）；架构文档 B-10 同步（已落地 M2-030）；compose 6 服务日志限额（max-size 10m/max-file 3）。
- **验证结果**：全量 go test 绿 + lint 0 + typecheck 0 + 产物链 PASS；全栈重建冒烟 5 端点 200 + docker inspect 实收日志限额。
- **里程碑**：**测试债/文档债/运维债三项清偿**——出口回归抓到的弱点全部闭环。
- **遗留**：vector/coherence-daemon（build 型）日志限额 P3。

## 2026-08-29 · 出口前最终回归（全绿）

- **回归抓到 2 问题并修复**：① platform 测试 NewDnsHandler 签名漏适配（M2-031 连带）；② Command result 3（empty）被拦截 → 租约空表 500（ctrl.go 容忍修复）。
- **修复后**：全量 go test/lint/typecheck 绿 + 冒烟 6 端点全 200（子网/名单/租约/设置/导出/诊断）。
- **里程碑出口完成**：实现说明 + 最终回归全绿，移交 review 列人工验收（51 张）。

## 2026-08-29 · 里程碑出口：编辑闭环与安全/日志/诊断域

- **做了**：`docs/dev/里程碑出口-编辑闭环与安全日志诊断域.md`——本段 12 卡（M2-021~M2-031、M5-012/031）的跨模块实现说明：出口范围/设计对接点/关键代码路径（黑名单两级检查/lease6 命令链/DNSSEC 渲染链/view 匹配闭包/编辑模式类型纪律）/各卡 e2e 证据表/事故与教训（ENOSPC ×2、锚点陷阱系列、镜像空行、JWT 验签顺序）/遗留与远期演进（refresh token 触发条件）。
- **里程碑状态**：板面功能项全清，review 列待人工验收。

## 2026-08-29 · M2-031 完成：解析测试台 view/模拟来源

- **做了**：spec clientIp/viewHint + gen；DnsHandler policyView 注入（ListPolicyGroups + netip CIDR 匹配）；main 装配（var 提前 + 闭包）；前端模拟来源 Input + viewHint Tag；diagnoseDns 签名 schema 化（修正 M2-014 违例）。
- **验证结果**：全链绿；e2e——分组 10.99.0.0/16→view-e2e，clientIp=10.99.1.1 → viewHint 命中，8.8.8.8 → 无提示。
- **里程碑**：**M2-014 遗留闭环——板面功能项全清**（提示页定制 P2 无需求信号，记录遗留）。
- **事故留痕**：ENOSPC 二次满盘——tmux（输出走内存）执行清理恢复 54G。

## 2026-08-29 · M5-031 完成：令牌黑名单多实例语义（Revoked 直查 PG）

- **做了**：Revoked 两级检查（内存→miss→PG EXISTS 点查）；架构决策落卡——多实例选 PG 点查不引 Redis（控制面量级/故障域/运维面三依据），refresh token 列远期演进；M5-012 卡遗留同步改写。
- **验证结果**：全链绿；e2e——真 token 200 → psql 直插 hash 模拟另一实例登出 → 同令牌 401 TOKEN_REVOKED（DB 点查路径命中）。
- **踩坑留痕**：吊销检查在 JWT 验签后——e2e 必须用合法签名 token + DB 直插 hash，假 token 活不到 Revoked。

## 2026-08-29 · M2-030 完成：DNSSEC 校验开关（B-10）

- **做了**：BuildConf trust-anchor 输出（IANA 根锚 DS 20326 常量，离线免联网）+ settings_test 断言；security 页 DNSSEC Switch 去灰置 + 文案 B-10；镜像重建部署。
- **验证结果**：全链绿；e2e——PUT dnssecValidate=true → unbound-rendered.conf 实收 trust-anchor + val-permissive-mode: no → false → yes + 锚消失（双向闭环）。
- **里程碑**：**B-10 闭环——架构文档最后一个 P2 功能块落地**。
- **踩坑留痕**：control-plane build 空行≠重建成功（Go 改动后 grep Built 确认）；渲染 conf 在 unbound-rendered.conf（include 拆分）。

## 2026-08-29 · M2-029 完成：日志 CSV 导出前端

- **做了**：ipam.ts exportLogsCsv（fetch blob + Content-Disposition 文件名解析）；search 页「导出 CSV」按钮（当前筛选 from/to/type/domain 映射 ExportLogsParams）；streamLogTail 确认无缺口（tail 页 EventSource 已接）。
- **验证结果**：typecheck 0 + 零外链 PASS；e2e——GET /logs/export 200 + Content-Disposition logs-20260828.csv + CSV BOM/表头/真实 unbound 数据行。
- **里程碑**：**日志域前端闭环**（检索/TopN/QPS/审计/tail SSE/CSV 导出全入口）。
- **踩坑留痕**：单行 import 块用 `} from ...` 锚点会切断——锚点前必须确认块形态。

## 2026-08-29 · M2-028 完成：双栈模板更新（编辑闭环 100%）

- **做了**：spec patch+DualstackTemplateUpdate+gen；Store 接口+ErrTemplateNotFound+Mem/Pg Update+handler UpdateDualstackTemplate；前端 updateDualstackTemplate+dualstack 页编辑模式。
- **验证结果**：全链绿 + e2e（建模板 → PATCH 200 → 回读 ipv6Prefix/expr/graceHours 全一致 → DELETE 204）。
- **里程碑**：**编辑闭环 100%**——全站管理页 CRUD 齐（子网/上游/转发/选项类/用户/组织/区域/记录/名单/模板）。
- **遗留**：无编辑类缺口；policy-groups 提示页定制等 P2 另列。

## 2026-08-29 · M2-027 完成：策略分组前端管理区

- **做了**：ipam.ts（PolicyGroupRow + listPolicyGroups/createPolicyGroup/compilePolicyGroup）；blocklist 页策略分组 Card（创建表单 cidrs 逗号分隔/listIds 多选 + Table + 编译按钮 + RPZ 编译结果展示）。
- **验证结果**：typecheck 0 + 零外链 PASS；容器重建 + API 全链回归（create → list 回读一致 → compile 真实产出 RPZ zone /var/lib/ipam/rpz/recursor.zone，entries=1 + reloadCommand）。
- **里程碑**：**blocklists 域全闭环**（名单查/建/删/同步 + 条目查/加/删 + 策略分组查/建/编译——前端入口全通）。
- **遗留**：提示页定制 P2。

## 2026-08-29 · M2-026 完成：前端 API 补齐（名单新建 + 区域删除）

- **做了**：ipam.ts（BlocklistCreateRow + createBlocklist/deleteDnsZone）；blocklist 页新建名单表单（name/kind/syncUrl，feed 条件显示订阅 URL）；records 页 zone Select 旁删区按钮（confirm 防误删 + 删除后自动回选）。
- **验证结果**：typecheck 0 + 零外链 PASS；容器重建 + deleteDnsZone 回归（create → delete 204 → 回读已消失）；createBlocklist 由 M2-024 e2e 覆盖（201）。
- **里程碑**：CRUD 完整性扫描后前端 API 面与 spec 对齐（blocklists 域全闭环：列表查/建/删/同步 + 条目查/加/删）。
- **踩坑留痕**：多行 HTML 标签中间插行破坏结构（vue-tsc 过但 vite build Invalid end tag）——插入前确认标签闭合位置。
- **遗留**：policy-groups 前端 P2；/logs/tail、/logs/export 前端 P2；dualstack update P2（删建可接受）。

## 2026-08-29 · M2-025 完成：封禁条目前端管理区

- **做了**：ipam.ts（BlocklistEntryRow 类型 + listBlocklistEntries/addBlocklistEntry）；blocklist 页条目 Card——列表行「条目」按钮 → 条目 Card（添加表单 pattern/triggerType/action + Table + 行删除按钮）。
- **验证结果**：typecheck 0 + 零外链 PASS；容器重建 + entries API 回归完整（add 201 → list 200 1 条 → delete 204 全通——页三入口数据源）。
- **里程碑**：**封禁列表页功能闭环**（列表查/删 + 条目查/加/删全前端入口）。
- **遗留**：列表级 PATCH 编辑（rename 影响 rpz zone 命名）P2；builtin 拒删 409 分支未 e2e 覆盖 P2。

## 2026-08-29 · M2-024 完成：封禁列表条目删除 + 列表删除

- **做了**：spec delete ×2（deleteBlocklist / deleteBlocklistEntry pattern query）+ gen；BlocklistRepo 接口 + errBlocklistNotFound + Mem/PG 双实现（级联删/自然键删）；handler 2 端点（404/409 builtin/204 与 errors.Is 404/204）；前端 ipam.ts 2 函数 + blocklist 页删除按钮。
- **顺手修**：PgBlocklistRepo.List `ORDER BY created_at` 引用不存在列（先前 bug）——GET 列表一直 500 → 改 ORDER BY name。
- **验证结果**：typecheck 0 + 全链绿 + 零外链 PASS；e2e 决定性——GET 列表 200（修复实证）、条目删除 204+回读消失、列表删除 204+回读消失（GET 200 真验证）。
- **遗留**：条目前端管理区 P2；列表级 PATCH 编辑（rename 影响 rpz zone 命名）P2。

## 2026-08-29 · M2-023 完成：上游多地址编辑

- **做了**：upstream/index.vue 三处——edit 预填 `addrs.join(',')`（修复多地址截断为第 1 个）、PATCH/POST 分支提交 `split(',').map(trim).filter(Boolean)`（逗号分隔多地址输入）。
- **验证结果**：typecheck 0 + build:ipam ✓ + sync ✓ + 零外链 PASS；e2e——PATCH 多地址 200 + 回读 2 条（223.5.5.5:53, 119.29.29.29:53）。
- **遗留**：封禁条目编辑 P2（删建可接受）；双栈页编辑 P2（spec 无 update）。

## 2026-08-29 · M5-012 完成：令牌黑名单持久化（重启恢复）

- **做了**：迁移 0015（auth_token_blacklist 表 + until 索引）；TokenBlacklist.AttachDB（清理过期 + 加载未过期 + 启用双写）；Add best-effort 双写（INSERT ON CONFLICT DO UPDATE）；main.go pool 非空时 AttachDB（失败降级内存 + [bl-load] 日志）。
- **验证结果**：platform 测试 ok + lint 0；e2e 决定性——登出 200 → 同令牌 401 → **重启 control-plane → 同令牌仍 401 TOKEN_REVOKED**（内存黑名单重启即清空，401 只能来自 DB 加载）→ 新登录 200；DB 层 schema_migrations 记录 0015_token_blacklist 已应用 + auth_token_blacklist 1 条。
- **里程碑**：M5-011 登出即吊销的**跨重启安全闭环**。
- **遗留**：多实例部署时 Revoked 仍走内存（单实例语义）——多实例需查库/Redis（P2）。

## 2026-08-29 · M2-022 完成：PD 租约查询（lease6 命令通道）

- **做了**：kea lease_cmds hook 加载（模板 + BuildConfig6 base 双侧）；spec GET /dhcp/leases6 + gen；kea CtrlAgent.Lease6List（result 0/3 校验 + leases 投影）；dhcp ListDhcpLeases6（lease6List 函数注入 + nil 安全）；main lease6Fn（keaCmd nil 安全 + 指针转换）；前端 subnets 页 PD 租约卡（实时查询 + 刷新）。
- **验证结果**：typecheck 0 + 全链测试绿 + lint 0 + 零外链 PASS；e2e——GET /dhcp/leases6 HTTP 200 + PD 租约回读全对（2406:172::100:0 IA_PD /80 duid/vlt）。
- **踩坑留痕**：① Kea hooks-libraries 键名是 "library"（错键 "lib" → DHCP6_INIT_FAIL 重启循环）；② Command(nil args) → "arguments": null → Kea get(string) on non-map → nil 省略字段；③ Kea 2.2 lease 字段名 valid-lft；④ lease6-add 须用 kea 现存 subnet-id（kea6 restart 后配置由 daemon 重推）。
- **里程碑**：**DHCPv6 PD 生命周期闭环**（委派池 → 租约可观测）。

## 2026-08-28 · M2-021 完成：编辑闭环批次（上游/转发规则/选项类）

- **做了**：三页编辑模式（editingId + edit() 预填 + PATCH/POST 分支 + 提交按钮动态文案 + 取消编辑按钮 + op 编辑按钮）；ipam.ts updateForwardRule（前端 API 补齐，spec/后端已有）。
- **验证结果**：typecheck 0 + 零外链 PASS + 全链测试绿 + lint 0；容器 e2e——上游 PATCH（223.6.6.6→223.7.7.7）200+回读一致、转发 PATCH（note）200+回读一致、选项 PATCH（routers data）200+回读一致。
- **里程碑**：**全站管理页编辑闭环**（子网/上游/转发/选项类/用户/组织）。
- **遗留**：双栈页编辑（spec 无 update——删建模式可接受）P2；封禁条目编辑（条目增删已有）P2。

## 2026-08-28 · M5-011 完成：登出即吊销（TokenBlacklist）

- **做了**：auth_blacklist.go（TokenBlacklist：SHA256 指纹 + 惰性过期清理）；AuthHandler 接入 bl（LogoutAuth 吊销有效令牌）；RBAC 中间件黑名单检查；单测锚定（黑名单语义 + 登出吊销 + guest 隔离性）。
- **验证结果**：全链测试绿 + lint 0；容器 e2e 决定性通过——登录 200 → GET 200 → logout 200 → 同令牌 GET **401 TOKEN_REVOKED** → 新登录 200。
- **里程碑**：M5-010/M5-011 安全闭环（禁用/改密 token_version 吊销 + 登出黑名单即时吊销）。
- **遗留**：黑名单重启清空（多实例部署需持久化）P2；「全设备登出」走 token_version Bump。

## 2026-08-28 · M2-020 完成：子网编辑功能（网关/DNS 修改闭环）

- **做了**：ipam.ts updateSubnet；子网页编辑模式（editingId + edit() 行预填 + PATCH/POST 分支 + 提交文案动态 + family 编辑禁用 + 操作列编辑按钮）。
- **验证结果**：typecheck 0 + 零外链 PASS + 全链测试绿 + lint 0；容器 e2e 决定性通过——PATCH 编辑网关（10.99.3.1→10.99.3.254）→ 200 → **kea dhcp4 config-get 实收 routers: 10.99.3.254**（编辑→DB→kea 实收闭环）→ 回读一致。
- **里程碑**：M2-019 网关/DNS 配置的编辑闭环完成（用户需求全链落地）。
- **遗留**：无。

## 2026-08-28 · M2-019 完成：子网级 DHCP 选项（网关/DNS，v4+v6）

- **做了**：spec Subnet/SubnetCreate/SubnetUpdate 加 gateway/dnsServers + gen；迁移 0014（runner 自动应用）；Subnet 结构/Pg 读写扩展；kea BuildConfig/BuildConfig6 的子网级 option-data 投影（v4 routers/domain-name-servers、v6 dns-servers）+ 单测锚定 ×2；前端子网表单（v4 网关+DNS/v6 DNS）+ 列表网关列。
- **验证结果**：全链测试绿 + lint 0 + typecheck 0 + 零外链；容器 e2e 决定性通过——v4 子网创建 201 → kea dhcp4 config-get 实收 subnet option-data（routers=10.99.3.1、domain-name-servers）；v6 子网 → kea6 恢复后重触发 → 日志实证 3 个 v6 子网实收。
- **里程碑**：**DHCP 下发参数完整化**（地址+掩码+网关+DNS 按子网后台可配，v4+v6）——用户报障的核心缺口收口。
- **遗留**：kea-dhcp6 control socket 不稳定（Kea 2.2）→ 升级跟进；子网级 option-data 的 PATCH 表单 P2。

## 2026-08-28 · M5-010 完成：令牌吊销（token_version）

- **做了**：迁移 0013（users.token_version）；JWTClaims.Ver + IssueTokenFor 五参；RBAC 中间件重构为认证+授权一体（全请求 enabled/ver 校验，SPA/login/logout 白名单）；UpdateUser 吊销触发（改密/停用/角色变更 → Bump）；Create 初始化 TokenVersion=1。
- **验证结果**：全链测试绿（含停用/改密吊销回归锚点 ×2）+ lint 0；容器 e2e——停用后存量令牌 GET 401、新密码登录 200。
- **里程碑**：M5-004 安全语义缺口收口（禁用/改密即吊销，不再等 24h 过期）。
- **遗留**：中间件每请求查库（PoC 可接受，缓存 P2）；登出黑名单 P2。

## 2026-08-28 · M2-018 完成：DHCPv6 管理落地（PD 前缀委派）——DHCP 菜单 6/6

- **做了**：spec Pool 加 prefixLen/delegatedLen + gen；迁移 0012（runner 自动应用）；Pool 结构/Pg 读写扩展（pd endAddr 推导）；kea BuildConfig6（subnet6：dynamic→pools、pd→pd-pools）+ DeploySubnet/applyDhcp dhcp6 config-set；前端 v6 池表单（kind+len）。
- **验证结果**：kea 单测锚定 + 全链绿 + lint 0 + typecheck 0 + 零外链；容器 e2e——v6 子网+PD 创建 201（endAddr 推导回填、keaSubnetId 递增）→ 全量下发 → **kea-dhcp6 日志实证 reconfigure 实收 2 个 v6 子网**。
- **里程碑**：**DHCP 菜单 6/6 收口**；v6 下发链（BuildConfig6→agent→dhcp6 socket）打通。
- **遗留**：kea-dhcp6 control socket 在 reconfigure 后不稳定（Kea 2.2 行为，force-recreate 恢复）→ 升级跟进；PD 租约生命周期 P2。

## 2026-08-28 · M3-009 完成：settings 渲染持久化路径打通（include 拆分）

- **做了**：confApplier 渲染产物落盘 /etc/unbound-rendered（宿主 config/unbound rw 挂载）；主 conf include 拆分（静态身份段归主 conf，删静态 auth-zone）；SettingsService.Update notify 收敛（失败回滚落库值）；BuildConf 静态身份解耦 + forward-addr @ 格式修正 + RenderSettingsBlock 指令正名（ip-ratelimit）；control-plane 镜像补 unbound 用户。
- **验证结果**：容器 e2e 决定性通过——PUT /dns/settings 200 → 渲染产物落盘 → **unbound get_option 实收 cache-max-ttl=300 / ip-ratelimit=400**；全链测试绿 + lint 0。
- **里程碑**：**DNS 域全链路真实生效闭环**（settings/上游/转发/解析记录/封禁 → 五源渲染 → checkconf → 原子落盘 → reload → unbound 实收）；settings 保存自 M2-011 以来首次真正生效。
- **遗留**：unbound 大版本升级时 include 合并语义回归验证 P2。

## 2026-08-28 · M3-008 完成：unbound 命令通道打通

- **做了**：ExecController.Conf（-c 注入）+ compose 挂载 config/unbound 只读 + IPAM_UNBOUND_CONF 注入；dev.sh 同步。
- **根因**：control-plane 内置 conf 是 TCP 模式（control-enable:no），unbound 容器真实配置是 unix sock 模式——客户端读错配置。
- **验证结果**：容器 e2e——POST /upstreams 201 无 X-Unbound-Warning（forward_add 经 sock 真实成功）。
- **里程碑**：**DNS 域运行时命令通道打通**（与 M3-007 的 Kea 侧对称收口，两大引擎真实下发全通）。
- **遗留**：settings 渲染 confPath 与 unbound 真实配置不同文件——conf 持久化路径设计后续卡。
## 2026-08-28 · M3-007 完成：Kea 绑定配置式下发（DHCP 域 kea 路径全收敛）

- **做了**：BuildConfigFull 投影 subnet reservations；LedgerService/SubnetService 双 apply 收敛（4 个 kea 命令调用点退役）；Create 的 KeaSubnetID 本地递增分配（修多子网撞车）；bulk 回滚缺口修复（含失败行自身）。
- **验证结果**：容器 e2e 决定性通过——bulk ok:true applied:3，kea config-get 实收 3 子网（id 唯一）+ reservations + option-data + client-classes，零错误。
- **里程碑**：**DHCP 域 kea 下发全面配置式收敛**（单一收敛点 applyDhcp：子网+池+选项+类+绑定一次 config-set 原子生效）；控制面单测含回滚回归锚点。
- **遗留**：reserve 池切分 P2；host reservation 租约联调 P2。
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
