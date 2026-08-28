# [M3-009] settings 渲染持久化路径打通（include 拆分+notify 收敛）

| 字段 | 内容 |
|---|---|
| ID | M3-009 |
| 状态 | review |
| 来源 | M3-008 遗留（settings 保存从未真正进 unbound 配置）；§2.3 三步走收口 |
| 负责 | opencode(backend/devops) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

settings（缓存 TTL/RRL/serve-expired）保存后渲染产物落到 unbound 容器可见路径并 reload 实收——修复「settings 参数从未真正生效」的架构缺口。

## 方案（include 拆分）

- 静态主 conf（config/unbound/unbound.conf）：运行时身份（username/chroot/directory/interface/port/access-control/logfile/local-data）+ remote-control（sock）+ 尾部 include 渲染产物；**删静态 auth-zone 段**（渲染 auth-zone 取代，消除 corp.local 双定义）
- 渲染产物：confApplier.apply 五源合成 → 原子落盘 /etc/unbound-rendered/unbound-rendered.conf（宿主 config/unbound/ rw 挂载）→ reload
- **BuildConf 静态身份解耦**：interface/access-control/logfile 归主 conf（渲染产物重复 interface 触发 unbound fatal 同端口双绑定）
- **SettingsService.Update 收敛**：block 快校验 → 落库 → notify（confApplier 全量渲染+落盘+reload）→ 失败回滚落库值

## 验收标准（可测）

- [x] PUT /dns/settings → 200；渲染产物落盘含 cache-max-ttl: 300
- [x] unbound get_option 实收 cache-max-ttl=300 / ip-ratelimit=400（决定性）
- [x] 全链测试绿 + lint 0（含 notify 失败回滚回归测试）
- [x] commit [M3-009]

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：SettingsService notify 收敛（Update 失败回滚落库值）；confApplier.confPath → /etc/unbound-rendered（宿主 config/unbound rw 挂载）；BuildConf 静态身份解耦（interface/access-control/logfile 四行删除）；主 conf include 拆分 + 删静态 auth-zone；control-plane 镜像补 unbound 用户（checkconf 校验 username 存在性）；group_add（宿主 gid + unbound gid 999）；forward-addr @ 格式修正（冒号非法）；CheckConf 统一 server: 段头（candidate 裸语句落对上下文）。
- **验证结果**：容器 e2e 决定性通过——PUT 200 → 渲染产物落盘 → **unbound get_option 实收 cache-max-ttl=300 / ip-ratelimit=400**（settings 参数首次真正进 unbound 运行态）；全链测试绿 + lint 0。
- **踩坑留痕（九连根因，剥洋葱式暴露）**：① 容器内置 conf 是 TCP 模式（control-enable:no）→ sock 模式注入（M3-008）；② checkconf fatal: user unbound 不存在 → 镜像补用户；③ logfile 目录不存在 → BuildConf 静态身份解耦；④ forward-addr 冒号格式 → netip 转 @；⑤ interface 双绑定 fatal → 渲染产物删静态身份段；⑥ CheckConf candidate 裸语句落错段上下文 → 统一 server: 段头；⑦ include 路径双容器视角错位 → 统一 /etc/unbound-rendered；⑧ 渲染目录写权限 → group_add 宿主 gid + 目录 g+w；⑨ ratelimit: yes 布尔非法 + ratelimit-per-ip 指令不存在（正名 ip-ratelimit）——M2-011 时代渲染缺陷（settings 保存从未真正生效过）。
- **遗留**：settings 渲染的 server: 段与主 conf 段合并语义依赖 unbound 标量后值——升级 unbound 大版本时回归验证 P2。
