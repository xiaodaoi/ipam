# LICENSES.md — 许可矩阵

> 合规红线（AGENTS.md 安全红线）：不引入 GPL 依赖。新增第三方依赖必须在此登记。

| 组件 | 许可 | 用途 | 商用结论 |
|---|---|---|---|
| ISC Kea 2.x | MPL-2.0 | DHCP 引擎 | ✅ 可用 |
| Unbound | BSD-3 | DNS 引擎 | ✅ 可用 |
| Vben Admin v5 | MIT | 前端基座 | ✅ 可用 |
| oapi-codegen / Spectral / openapi-diff / Scalar | MIT/Apache 类 | 工具链（§12.5） | ✅ 可用 |
| gin-gonic/gin | MIT | HTTP 框架（D2） | ✅ 可用 |
| oapi-codegen/runtime | Apache-2.0 | 生成代码运行时依赖 | ✅ 可用 |
| miekg/dns | BSD-2-Clause | DNS 报文构造/解析（M2-014 解析测试台） | ✅ 可用 |
| PowerDNS | GPL-2.0 | ~~已否决~~（D4） | ❌ 不引入 |
| mosdns 系 | GPL-3.0 | ~~已否决~~（§11.2） | ❌ 不引入 |

待登记：Go/前端具体依赖在 M0-004/M0-007 落地时补充。
