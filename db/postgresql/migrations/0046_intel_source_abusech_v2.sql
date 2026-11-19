-- 0046: abuse.ch 2026-10 关闭无鉴权下载，预设升级到需 Auth-Key 的 v2 导出。
-- 仅当 URL 仍是旧的预设值时才改写，避免覆盖用户自定义地址。
-- {secret} 为运行期占位符：拉取时替换，库内与接口不回显密钥。

UPDATE intel_source
   SET url = 'https://urlhaus-api.abuse.ch/v2/files/exports/{secret}/hostfile.txt',
       auth_header = '',
       updated_at = now()
 WHERE key = 'abusech-urlhaus'
   AND url IN (
     'https://urlhaus.abuse.ch/downloads/hostfile/',
     'https://urlhaus.abuse.ch/downloads/csv_recent/'
   );

UPDATE intel_source
   SET url = 'https://threatfox-api.abuse.ch/v2/files/exports/{secret}/hostfile.txt',
       auth_header = '',
       updated_at = now()
 WHERE key = 'abusech-threatfox'
   AND url IN (
     'https://threatfox.abuse.ch/export/csv/recent/',
     'https://threatfox.abuse.ch/export/json/recent/',
     'https://threatfox.abuse.ch/downloads/hostfile/'
   );
