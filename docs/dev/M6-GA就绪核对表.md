# M6 GA 就绪核对表

| 字段 | 内容 |
|---|---|
| 版本 | v1.0 |
| 日期 | 2026-11-04 |
| 用途 | 商业化 GA 交付评审：逐条核对 `docs/pm/plans/M6-商业化就绪实施计划.md` §11 的 10 条 Gate |
| 事实源 | M6 实施计划 §11/§12、`docs/dev/M6-*` 实现说明、`docs/manual/*`、`.github/workflows/*`、根 `Makefile` 与 `scripts/*.sh` |

## 1. 总体结论

10 条 Gate 中，**1、3、4、5、7、9 已在本机可复现地满足**；**2、6、8 为部分满足**；**10 未开始**。因此本表**不支持「全部完成」**的结论，当前状态应表述为「单机可交付候选，尚差外部环境验证与客户试用」。

2026-11-04 已完成发布前实测：构建/测试/lint/E2E/契约/安全抽查/审计链/`/metrics` 令牌/License 激活与超限/备份与恢复机制/离线包校验均通过；并完成三项加固——备份导出超时与 Parquet 完整性（M6-028）、离线包密钥红线（M6-029）、备份增量复用历史月份（M6-030）。

| 分类 | Gate | 说明 |
|---|---|---|
| 已满足 | 1 安全基线、3 License、4 监控/告警/日志外发、5 账号体系、7 五类文档、9 E2E/压测 | 证据与本地命令见 §2 |
| 部分满足 | 2 备份/恢复/回滚、6 DNS/DHCP/IPAM 缺口、8 漏洞/SBOM/签名 | Gate 2 的 HA RTO≤30s 未验证；Gate 6 的 B5 未关闭；Gate 8 的扫描/签名仅 CI 生效 |
| 未开始 | 10 客户试用 2 周 | 需客户环境与连续 2 周观察窗口 |

**外部环境待验证（关闭前不得勾选对应 Gate）：**

- **M6-003B**：双机主备，需第二台独立主机/VM；单机不做伪 HA。
- **M6-012B**：Helm Chart，需 K8s 集群验证；不阻塞五类文档，但影响 Helm 交付形态。
- **M6-010B5**：IPv6 RA、基于 circuit-id 选池、冲突探测，需真实双栈/中继环境。
- **CI 专属扫描**：trivy 镜像 CVE、Syft SBOM、cosign keyless 签名与多架构构建；本机外网不可达，仅能在 CI/联网环境验证。
- **客户试用**：2 周无 P0/P1 事故，需客户环境执行并输出试用报告。

> 验证约定：下表默认 `BASE=https://127.0.0.1:8443`，`TOKEN` 为管理员登录返回的 `accessToken`。`make dev` 起的开发栈为 **HTTP**，需把 `https://` 改为 `http://` 并去掉 `curl -k`；生产/Compose 栈按 `docs/manual/install.md` §2/§4 使用 HTTPS。

## 2. Gate 逐条核对（对应 §11 的 10 个 checkbox）

| Gate | 现状 | 证据 | 本地验证命令 | 剩余阻塞 |
|---|---|---|---|---|
| **1** 全站 HTTPS，无默认口令，密钥不入库，审计可取证 | **已满足**（M6-001 / M6-017 / M6-006）。Compose 生产栈由 `install.sh` 生成自签证书并执行 HTTPS 冒烟；正式 CA 证书见 `install.md` §4。`make dev` 开发栈为 HTTP `127.0.0.1:8443`，仅限本机调试。空库随机 admin 口令；JWT/证书由部署生成，密钥不入库；审计为 SHA256 链，支持按时间窗完整性校验。**2026-11-04 实测**：审计链校验 `checked=259 / valid=true`。 | `M6-001-安全基线实现说明.md`、`M6-017-网络安全加固实现说明.md`、`M6-006-审计合规实现说明.md`；`install.md` §2/§4；`operations.md`「网络安全加固」 | `curl -sk -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/subnets"` → `401`<br>`openssl s_client -connect 127.0.0.1:8443 -tls1_1 </dev/null` → handshake failure<br>`curl -skD /tmp/ipam-headers "$BASE/" -o /dev/null && grep -iE -e "strict-transport" -e "content-security" -e "x-frame" /tmp/ipam-headers` → 有安全头<br>`curl -sk "$BASE/api/v1/audits/verify?from=2026-01-01T00:00:00Z" -H "Authorization: Bearer $TOKEN"` → `broken=0` | 无代码阻塞。生产交付前须替换 CA 证书，并按 `install.md` §3 开启强制改密。 |
| **2** 备份/恢复/升级回滚演练通过；HA 切换 RTO≤30s | **部分满足**。备份/恢复/升级回滚已实现（M6-002：PG+配置+CH 日志备份、下载、恢复、每日策略与保留、管理页、单测）；升级前快照与回滚步骤见 `upgrade.md`；M6-003A 提供 systemd 自启与每分钟看门狗。**2026-11-04 实测**：手动备份 `success`（4 个月 / 623,615,501 字节 / 4 个 Parquet 均以 `PAR1` 结尾，`2026-08` 读回 3932 行与库内一致）；并修复导出超时与「HTTP 200 + 半截文件」完整性漏洞（M6-028）。**增量复用**（M6-030）：第二次备份 10s（首次 55s），`2026-08/09/10` 硬链接同 inode、仅当月重导，复用目录实际占用 65M。**非破坏性恢复演练**（2026-11-04）：`postgres.dump` 用与 `PgRunner.Restore` 相同的 `pg_restore --clean --if-exists --no-owner --no-privileges` 恢复到独立库 `ipam_restore_verify`，核心表行数一致（users 5/5、roles 4/4、org_group 5/5、subnet 17/17、address_pool 16/16、reservation 14/14、dns_record 5/5、upstream 13/13、dhcp_options 4/4；operation_audit 185→269、schema_migrations 41→42、backup_record 5→1 等差异均来自备份时间点之后的增量，符合预期）；ClickHouse `logs-2026-10.parquet` 恢复到临时表 21,683,463 行 == 库内该月行数。**HA 未验证**：M6-003B `blocked`，需第二台独立主机，单机不做伪 HA。 | `M6-002-备份恢复实现说明.md`、`M6-028-备份导出超时与健壮性.md`、`M6-030-备份复用历史月份.md`；`upgrade.md` §升级/回滚；`operations.md`「备份/恢复/单机韧性/单机 HA 边界」；M6 计划 §12（M6-003B blocked） | `curl -sk -X POST "$BASE/api/v1/system/backups" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"note":"ga_rehearsal"}'`<br>`curl -sk "$BASE/api/v1/system/backups" -H "Authorization: Bearer $TOKEN" -o /tmp/ipam-backups.json && jq -r '.[0].status' /tmp/ipam-backups.json` → `success`<br>`curl -sk -X POST "$BASE/api/v1/system/backups/<id>/restore" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"confirm":true}'`<br>`systemctl status ipam-watchdog.timer`（已安装时） | **M6-003B**：需 2 台独立主机 + keepalived + Kea hot-standby + config-replicate；`kill -9` 主节点 RTO≤30s 与无重复租约未实测。HA 未验证前本条不勾选。**恢复演练**：`POST /system/backups/{id}/restore` 为破坏性操作，需维护窗口；当前已验证「备份成功 + Parquet 可读回」，尚未执行整机恢复。 |
| **3** License 可用，超限软降级 | **已满足**（M6-004）。Ed25519 离线 License、激活/移除页、签发 CLI、community 回退；终端计量（近 24h 去重 MAC）与超限仅告警不中断；指标与告警已接入。**2026-11-04 实测**：签发并激活 `LIC-GA-REHEARSAL`（enterprise/5000 台）→ `licensed=true`、`terminalsUsed=235/5000`；改签 `max=1` → `terminalsOverLimit=true` 且服务继续可用；`DELETE` 后回 community。 | `M6-004-License实现说明.md`；`operations.md`「授权与终端计量」；`openapi/paths/license.yaml` | `go run ./cmd/licensegen -gen-key` → private/public<br>`IPAM_LICENSE_PRIVATE_KEY=<private> go run ./cmd/licensegen -id LIC-GA -to "GA-Rehearsal" -edition enterprise -features ha,ldap,siem -max 5000 -days 365 -out /tmp/license.json`<br>`curl -sk -X PUT "$BASE/api/v1/system/license" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"license\":$(jq -Rs . < /tmp/license.json)}"`<br>`curl -sk "$BASE/api/v1/system/license" -H "Authorization: Bearer $TOKEN"` → 版本/到期/上限正确 | 无。商业功能按 `features` 拦截属后续接入，不影响 §11 三条验收。 |
| **4** 监控/告警/日志外发可用 | **已满足（基础范围）**。M6-005：`/metrics`（令牌保护）、Prometheus/Grafana、6 条告警、Alertmanager 示例、NTP 预检（2026-11-04 实测：设置 `IPAM_METRICS_TOKEN` 后无令牌/错令牌均 401、正确令牌 200）；M6-008/008A：Syslog/Kafka/Webhook、采样、90% 自动降级 / 85% 恢复、积压与丢弃告警；M6-015：查询内存/落盘护栏、count 缓存、容量/新鲜度指标。 | `M6-005-可观测与告警实现说明.md`、`M6-008-日志外发实现说明.md`、`M6-015-数据长期稳定性实现说明.md`；`operations.md`「日志外发/日志容量」 | `curl -sk "$BASE/metrics" -H "Authorization: Bearer $(cat observability/metrics_token)" -o /tmp/ipam-metrics && grep -E -e 'ipam_build_info' -e 'ipam_component_up' -e 'ipam_log_disk_bytes' /tmp/ipam-metrics` → 指标存在且组件为 1<br>`GRAFANA_ADMIN_PASSWORD='...' docker compose -f observability/compose.observability.yaml --profile observability config` → 退出 0<br>页面：日志中心 → 外发与采样，保存后确认 Vector 热加载 | 无。SMTP/Webhook/钉钉地址需按现场填写，属部署配置。 |
| **5** 账号体系（LDAP/2FA/角色赋值）可用 | **已满足**。M6-007A：TOTP 2FA（含一次性恢复码）、自定义角色多选、注销全部会话；M6-007B：LDAP/AD 目录登录、组→角色映射、影子账号、连通性测试；M6-007C：会话列表与单会话强制下线。 | `M6-007-账号体系实现说明.md`、`M6-007A-TOTP恢复码实现说明.md`、`M6-007B-LDAP目录登录实现说明.md`、`M6-007C-会话管理实现说明.md`；`operations.md`「LDAP / AD」「会话管理」 | `go test ./internal/module/platform -run 'TestTOTP' -v` → 全绿<br>`go test ./internal/module/platform -run 'TestLogin_LDAP' -v` → 全绿<br>`go test ./internal/module/platform -run 'TestSession' -v` → 全绿<br>`node tests/e2e/run.mjs` → 覆盖「目录服务设置」「个人中心登录会话」「系统账号管理」步骤 | 无。真实 AD/LDAP 联调需客户目录与网络策略；无目录时由单测覆盖。 |
| **6** DNS/DHCP/IPAM 缺口关闭，关键路径有 E2E | **部分满足**。已关闭：M6-009A/B1/B2（TXT/SRV/MX、hosts、rebind、泛域名、DoT、secondary/AXFR、提示页、时段、内置库、CSV）、M6-010A/B1/B2/B3/B4（租约、利用率、发现、台账、option82 展示）、M6-022（VLAN/站点/用途/负责人）、M6-023。**未关闭**：M6-010B5（IPv6 RA、基于 circuit-id 选池、冲突探测）仍为 backlog。 | `M6-009-DNS功能补齐实现说明.md`、`M6-023-DNS能力补齐实现说明.md`、`M6-010-租约管理实现说明.md`、`M6-010-利用率采样实现说明.md`、`M6-010-终端发现实现说明.md`、`M6-010-台账元数据实现说明.md`；M6 计划 §12 | `go test ./internal/module/dns ./internal/module/dhcp ./internal/module/ipam ./internal/module/utilization` → 全绿<br>`dig @127.0.0.1 a.wild.<已配置域>` → 泛解析命中（用例见 M6-023 文档）<br>`node tests/e2e/run.mjs` → 子网/租约/发现/利用率/DNS 页面通过 | **M6-010B5**：RA、circuit-id 选池、冲突探测需真实双栈/中继/RAW 探测环境，当前无此环境。 |
| **7** 安装/升级/运维/用户/API 五类文档齐全 | **已满足**（M6-012A）。README + 安装/升级/运维/用户/API 五类文档 + 离线包脚本均存在；API 以 `openapi.json` 为源。**2026-11-04 实跑**：离线包 10 镜像 + bundle 生成成功、`sha256sum -c` 11 项全 OK；交付包已排除 `.env`/`certs/`/证书私钥，并内置自检（M6-029）。Helm（M6-012B）`blocked`，不属于五类文档，但影响交付形态。 | `README.md`；`docs/manual/{install,upgrade,operations,user-guide,api}.md`；`scripts/package-offline.sh`、`scripts/check-bundle-secrets.sh`（M6-029）；M6 计划 §12（M6-012A done / M6-012B blocked） | `for f in install upgrade operations user-guide api; do if [ ! -s "docs/manual/$f.md" ]; then echo "MISSING $f"; fi; done` → 无输出<br>`curl -sk "$BASE/openapi.json" -o /tmp/ipam-openapi.json && jq -r .openapi /tmp/ipam-openapi.json` → `3.1.x`<br>`make package-offline && (cd dist/offline && sha256sum -c SHA256SUMS)` → 全 `OK` | **M6-012B**：Helm 需 K8s 集群验证（外部环境）。 |
| **8** 无高危漏洞，镜像有 SBOM 与签名 | **配置就绪，验证受限于 CI/外网**。`release.yml`：6 个镜像 `linux/amd64,linux/arm64`、`sbom: true`、Syft（anchore/sbom-action）SPDX artifact、cosign keyless 签名、trivy image HIGH/CRITICAL 门禁；`security.yml`：govulncheck、gosec、pnpm audit、trivy fs、trivy config；M6-019 完成运行时加固与误配修复。**本机外网不可达，CVE 扫描与签名无法本地完成**；尚无 tag 发布记录。 | `.github/workflows/release.yml`、`.github/workflows/security.yml`；`M6-019-镜像安全扫描与运行时加固实现说明.md`；`M6-011-工程质量实现说明.md`；`.trivyignore.yaml` | `grep -nE -e 'platforms:' -e 'sbom: true' -e 'cosign sign' -e 'trivy' .github/workflows/release.yml` → 命中<br>`make scan-images`（需 trivy 或 docker+网络；`bash scripts/scan-images.sh --strict` 缺工具判失败）<br>`trivy config --severity HIGH,CRITICAL deploy` → 0 命中（例外见 `.trivyignore.yaml`） | **CI 专属扫描**：需联网 CI 跑 trivy/SBOM/cosign；需一次 `v*` tag 发布验证 GHCR 权限、arm64 构建与签名可校验。 |
| **9** 8 条主链路 E2E 全绿，压测报告达标 | **已满足**。`node tests/e2e/run.mjs` 当前 **17/17**，含新增「英文界面切换（i18n）」步骤；M6-025 文档记录当时 16/16，新增该步骤后为 17/17。i18n 同时满足 71 文件 0 硬编码中文、zh/en 双语、切换即时生效且刷新保持。压测见 M6-016：全链路 0 错误，dashboard 8511 RPS / p50 1.07ms，ClickHouse 5M 行/天容量与 3s 查询预算有 40 倍余量。 | `M6-025-i18n实现规范与术语表.md`；`M6-016-全链路压测与容量报告.md`；`tests/e2e/run.mjs`；`tests/e2e/artifacts/result.json`；`operations.md`「日志容量与长期运行」 | `node tests/e2e/run.mjs` → `E2E PASS: 17/17 步通过`<br>`IPAM_RATE_LIMIT=0 make load-test` → 0 错误并生成压测报告<br>`make capacity-test` → 输出行/秒、压缩比、查询耗时（影子表自动 DROP） | 无功能阻塞。压测数字依赖硬件规格，交付前需在目标规格机复测并留档。 |
| **10** 客户试用 2 周无 P0/P1 事故 | **未开始（外部依赖）**。当前无客户环境与试用记录；不能在实验室环境替代。 | M6 计划 §11 第 10 项；§12 无对应完成记录 | 无本地命令。客户环境连续运行 2 周，按 P0/P1 定义记录事故数与处置时长，输出试用报告并归档。 | **客户试用**：需客户环境、2 周窗口与验收人；P0/P1 非零即不勾选。 |

> 行数说明：上表 10 行，严格对应 M6 实施计划 §11 的 10 个 checkbox，顺序一致。

## 3. 发布前检查清单

- [x] **工具链自检**：`make doctor`（或 `bash scripts/doctor.sh`）→ 关键工具均可用；CI 侧用 `--strict` 收紧，不允许关键项静默跳过。2026-11-04 实跑 exit 0；`corepack`/`spectral` 显示 MISSING（脚本注明可由 npx 替代），因此尚未达到 `--strict` 全绿。
- [x] **构建**：`make build` → Go 二进制与前端产物生成成功，无关键部分被跳过；产物位于 `bin/` 与 `cmd/control-plane/webui/dist/`。2026-11-04 `bash scripts/make-part.sh build` exit 0，生成 `bin/{control-plane,coherence-daemon,licensegen}`，前端 `webui synced (5.3M)`。
- [x] **测试**：`make test` → `go test` 全绿且 `pnpm test` 覆盖率门槛通过；新增逻辑有单测。2026-11-04 `bash scripts/make-part.sh test` exit 0：`go test ./...` 全绿、前端 7 文件/33 用例与覆盖率门槛通过、离线图标一致性 OK。
- [x] **静态检查**：`make lint`（或 `bash scripts/make-part.sh lint`）→ golangci-lint `0 issues`、stylelint/oxfmt/oxlint/eslint 均 0 error（warning 逐条确认）。2026-11-04 已归零（M6-027），连跑两次仍 exit 0；规则边界见 `docs/dev/M6-027-静态检查与格式化约定.md`。
- [x] **契约一致**：`make gen-check` → `api/gen` 与 `api/openapi` 无 diff；禁止手改 `api/gen/**`。2026-11-04 `bash scripts/gen-openapi.sh --check` → `OK: api/gen 与 spec 一致`。
- [x] **E2E**：`IPAM_E2E_USER=admin IPAM_E2E_PASSWORD='<初始口令>' node tests/e2e/run.mjs` → `E2E PASS: 17/17 步通过`；失败时查 `tests/e2e/artifacts/result.json` 与截图。2026-11-04 实跑 17/17（含英文界面切换与图标零外链）。
- [ ] **备份→恢复演练**：`POST /system/backups` 创建 `note=pre_release`，等待 `status=success`（2026-11-04 已验证：`success`、4 个月、623,615,501 字节、Parquet 尾部 `PAR1`、`2026-08` 读回 3932 行与库一致；M6-028。M6-030 已验证历史月份硬链接复用、第二次备份 10s）；在维护窗口 `POST /system/backups/{id}/restore` 携带 `{"confirm":true}`；恢复后抽查账号、子网、租约与日志条数。期望数据一致，无报错。**恢复机制已验证（非破坏性）**：PG dump 恢复到独立库、核心表行数一致；2026-10 Parquet 恢复 21,683,463 行与库一致（2026-11-04）。**原地破坏性 restore 仍待维护窗口执行**。
- [x] **License 激活**：按 Gate 3 命令签发并 `PUT /system/license`；`GET /system/license` 状态有效，`/metrics` 可见 `ipam_license_terminals_used` / `ipam_license_terminals_limit`；人为超限时仅出现告警与「终端超限」标签，服务不中断。2026-11-04 实测：Ed25519 签发 `LIC-GA-REHEARSAL`（5000 台上限）激活 200，`terminalsUsed=235/5000`；改签 `max=1` 后 `terminalsOverLimit=true` 且 `/subnets` 仍 200；`DELETE` 后回 community。
- [x] **`/metrics` 抓取**：`curl -sk "$BASE/metrics" -H "Authorization: Bearer $(cat observability/metrics_token)"` → 返回 `ipam_build_info`、`ipam_component_up`（postgres/clickhouse/kea 均为 1）、宿主与日志指标；未配置令牌时应仅限内网并关注启动告警。2026-11-04 开发栈实测 35 个 `ipam_*` 指标；临时设置 `IPAM_METRICS_TOKEN` 后验证：无令牌 401、错令牌 401、正确令牌 200，随后恢复无令牌的开发默认（生产须配令牌或仅内网暴露）。
- [x] **安全抽查**：未授权 `GET /api/v1/subnets` → 401；`TRACE /` → 405；2 MB 请求体 → 413；`openssl s_client -tls1_1` 握手失败；响应含 HSTS/CSP/X-Frame-Options 等安全头（详见 `operations.md`「网络安全加固」）。2026-11-04 开发栈（HTTP）实测 401/405/413，CSP、Referrer-Policy、X-Content-Type-Options、X-Frame-Options 均存在；HSTS/TLS 版本需在 HTTPS 生产栈执行。
- [ ] **离线包校验**：`make package-offline` → `dist/offline/` 生成镜像 tar、`ipam-bundle.tar.gz`、`SHA256SUMS`；`cd dist/offline && sha256sum -c SHA256SUMS` 全部 `OK`（2026-11-04 已验证 11 项全 OK，2.7GB）；脚本内置密钥自检（M6-029）→ `OK: 交付包不含 .env/证书/私钥`；按脚本提示在离线环境加载镜像并执行 `install.sh`。**离线环境实装仍待目标机执行**。
- [ ] **镜像扫描/SBOM/签名**：联网或 CI 执行 `make scan-images` → 无 HIGH/CRITICAL 可修复漏洞；打 `v*` tag 后确认 `release.yml` 的 `image-scan` 通过、SBOM artifact 已上传、`cosign verify` 可验证签名。本机无外网时此项标记为「待 CI」，不得默认通过。**当前状态：待 CI（本机外网不可达）**。

## 4. 维护约定

1. **单一事实源**：本表只做 Gate 状态映射，不新增决策。架构/范围变化仍按 `AGENTS.md` 走 ADR 或架构文档章节修订。
2. **同步更新**：任一 Gate 状态、证据或阻塞变化时，同一次提交内同时更新本表与 `docs/pm/plans/M6-商业化就绪实施计划.md` §12；commit 带对应 `[M6-nnn]`。
3. **关闭证据**：Gate 勾选前必须有本表「本地验证命令」的成功输出、CI run 链接或外部试用报告；禁止只写「已验证」而无证据链接。
4. **外部 Gate**：M6-003B / M6-012B / M6-010B5 / CI 专属扫描 / 客户试用的关闭，须先将 §12 状态从 `blocked`/`backlog` 改为 `done` 并附证据，再更新本表。
5. **风险联动**：里程碑出口复盘时同步 `docs/pm/risks.md`；本表不替代风险登记簿。
