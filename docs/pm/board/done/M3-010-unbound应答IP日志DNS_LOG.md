# M3-010 unbound 应答 IP 日志（DNS_LOG python 模块）

## 目标

DNS 日志拿不到解析结果 IP（`answer_ip` 全空）：unbound 1.26.0 的 `log-replies` 行不含应答区（实测 verbosity≤4 均无），vector 正则无从提取。引入 unbound python 模块在解析完成后从 `qstate.return_msg` 提取应答区，落库 `resolve` 事件并携带 `answer_ip`。

## 实施记录

### 改动

- `deploy/images/unbound/Dockerfile`：构建依赖加 `python3-dev python3 swig`；configure 加 `--with-pythonmodule`（jammy 需 `PYTHON_VERSION=3.10`，否则 configure 找不到 `python` 可执行）
- `config/unbound/dns_log.py`（新增）：pythonmod 四件套 `init(id,cfg)/deinit/operate/inform_super`（inform_super 与 init 的 cfg 形参缺一即 fatal）；MODDONE 时解析 rrsets 的 rr_data（前 2 字节 RDLENGTH），A/AAAA/CNAME 转 text，顶层输出 `answer_ip`（首个 A/AAAA，即 CNAME 链终点）；return_msg 为空（SERVFAIL）补记 rcode=SERVFAIL
- `config/unbound/unbound.conf`：`module-config: "python iterator"`；`python: python-script:`（注意选项名是 python-script 不是 script）；`log-replies: no` 去重
- `config/vector/vector.toml`：新增 `parse_dns_log`（正则抽 `DNS_LOG {...}` → parse_json → 顶层字段直读）；`parse_unbound` 收敛为仅查询行。VRL 0.46 无 `for` 循环（保留字）、无 `to_timestamp`——首记录选择逻辑放 Python 侧

### 关键决策（ADR 级）

- `log-replies: no`：DNS_LOG 为其超集（rcode/client_ip/qname/qtype/answer_ip），开启则每条递归查询双份 resolve 事件
- local-data 在 worker 层直接应答不过模块链 → 仅 dns_query 无 resolve 事件（静态已知数据，可接受）
- answer_ip 列为 Nullable(IPv6)，IPv4 以 `::ffff:x.x.x.x` 映射形式存储（查询侧 toString 兼容）

### 验证

- `unbound-checkconf` 无错；dig 递归（baidu：CNAME 链 → answer_ip=153.3.238.28）、NXDOMAIN（resolve+rcode）、local-data（仅 dns_query）三类均符合设计
- CH 无重复 resolve 事件；answer_ip 正确入库；后端 `ch_store.go` 既有 `coalesce(toString(answer_ip))` 直通前端
