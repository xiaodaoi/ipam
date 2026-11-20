-- 0049: 清理 RPZ 通配规则名产生的假报警（M6-040 修复）
-- 早期解析把白名单 rpz-passthru 记为 blocked，并把命中的通配规则（如
-- *.corp.example）当成查询域名写入报警；这类记录并非真实被拦截访问。
-- 采集器已在两处修复（Vector 解析跳过 passthru、报警采集过滤通配名），
-- 这里清理历史遗留记录，避免报警中心继续展示。

DELETE FROM alert WHERE source = 'dns' AND domain LIKE '%*%';
