# [M2-031] 解析测试台 view/模拟来源

| 字段 | 内容 |
|---|---|
| ID | M2-031 |
| 状态 | review |
| 来源 | M2-014 遗留（架构文档 §571）：测试台仅 name/type 查询——缺「模拟来源→view 分流提示」（策略分组 cidrs→viewName 的就地验证） |
| 负责 | opencode(frontend) |
| 创建 | 2026-08-29 |
| 更新 | 2026-08-29 |

## 方案

1. spec：DiagnoseRequest 加 `clientIp`（可选）+ DiagnoseResult 加 `viewHint`（可选）+ gen
2. 后端：DnsHandler 注入 policyView 闭包（blRepo.ListPolicyGroups + netip CIDR Contains，首中返回 viewName）；DiagnoseDns 查询后附加 viewHint；main 装配（var blRepo 声明提前供闭包捕获）
3. 前端 security 页测试台：「模拟来源 IP」Input + viewHint Tag；顺手修正 diagnoseDns 手写内联参数为 schema 引用（M2-014 违例）

## 范围说明

模拟来源仅用于 view 分流**提示**（不伪造真实查询源——UDP 源伪造需 raw socket，超出演示语义）；封禁生效验证走既有真实查询路径（查被封禁域名看 NXDOMAIN）。

## 验收标准（可测）

- [x] typecheck/全链绿 + 零外链 PASS
- [x] e2e：分组 cidrs=10.99.0.0/16 → clientIp=10.99.1.1 → viewHint=view-e2e；8.8.8.8 → 无提示
- [x] 文档三件套 + commit [M2-031]

## 实施记录（追加式，勿删旧条目）

### 2026-08-29 · 会话1
- **做了**：spec clientIp/viewHint + gen；DnsHandler policyView 函数注入 + DiagnoseDns 查询后附加 viewHint；main.go（net/netip import + var blRepo 声明提前 + policyViewFn 闭包 + NewDnsHandler 双参 + blRepo 去重赋值）；前端 testClientIp Input + viewHint Tag + diagnoseDns 签名 schema 化。
- **验证结果**：go build/test/lint 全绿 + typecheck 0 + 零外链 PASS；容器重建 + e2e 决定性——建分组 201（cidrs=10.99.0.0/16→view-e2e）→ diagnose clientIp=10.99.1.1 → viewHint=view-e2e（rcode NOERROR）→ clientIp=8.8.8.8 → viewHint 无 → psql 清理。
- **踩坑留痕**：① spec anchor 段尾有残留行（type 段 description 在 enum 后，探索时 sed 截断未看到，replace 后悬空成 YAML 重复键）——**replace 前必须看完整段**；② 单行 import 块 `} from` 锚点切断（M2-029 同款）；③ diagnoseDns 手写内联参数违例顺手修正为 schema 引用；④ 磁盘第二次满盘（ENOSPC）——bash 工具自身失败时改用 tmux（输出走内存）执行清理，journalctl vacuum + /tmp 清理恢复 54G。
- **遗留**：无——**M2-014 遗留闭环，板面功能项全清**；提示页定制 P2 无需求信号，记录遗留。
