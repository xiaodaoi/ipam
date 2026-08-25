# internal/ — Go 业务代码（Gin 单体模块化）

- `module/{ipam,dhcp,dns,coherence,logquery,platform}`：业务模块，与主导航权限码命名空间一一对应（§13.4）
- `engine/{kea,unbound}`：引擎编排器——配置生成→校验→生效→失败回滚三步走（§2.2/§2.3）
- `rpz/`：名单编译器（blocklist 表 → zonefile 增量编译）
- `prober/`：上游探活与摘除回切（F-R4）
- `pkg/`：公共库

模块职责详见架构文档 §2、§5、§14。
