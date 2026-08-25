# config/ — 引擎配置模板

各数据面组件的配置模板，按部署形态渲染：

- `unbound/`：unbound.conf 模板、view/rpz 片段
- `kea/`：kea-dhcp4/6、ctrl-agent JSON 模板与 hook 配置
- `keepalived/`：VRRP 主备模板
- `vector/`：日志采集与 VRL 归一化规则（→ClickHouse）

原则：镜像通用、配置按环境渲染，交付物不含环境耦合（§14 原则 5）。
