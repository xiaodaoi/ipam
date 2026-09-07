// DHCP 标准选项预设（RFC2132 / RFC8415 常用项；name 为 Kea 标准选项名）。
export interface DhcpOptionPreset {
  code: number;
  name: string;
  label: string; // 中文名 + 说明
}

export const DHCP4_OPTION_PRESETS: DhcpOptionPreset[] = [
  { code: 2, name: 'time-offset', label: '时间偏移（UTC 秒差，已废弃）' },
  { code: 4, name: 'time-servers', label: '时间服务器（RFC868 旧协议，非 NTP）' },
  { code: 7, name: 'log-servers', label: '日志服务器（Syslog）' },
  { code: 15, name: 'domain-name', label: '域名后缀（如 corp.local）' },
  { code: 19, name: 'ip-forwarding', label: 'IP 转发开关（0/1）' },
  { code: 28, name: 'broadcast-address', label: '广播地址' },
  { code: 42, name: 'ntp-servers', label: 'NTP 服务器' },
  { code: 44, name: 'netbios-name-servers', label: 'NetBIOS 名称服务器' },
  { code: 66, name: 'tftp-server-name', label: 'TFTP 服务器名（PXE）' },
  { code: 67, name: 'bootfile-name', label: '启动文件名（PXE）' },
];

export const DHCP6_OPTION_PRESETS: DhcpOptionPreset[] = [
  { code: 7, name: 'preference', label: '服务器优先级（0-255，越大越优先）' },
  { code: 21, name: 'sip-server-dns', label: 'SIP 服务器（域名列表）' },
  { code: 23, name: 'dns-servers', label: 'DNS 服务器（IPv6）' },
  { code: 24, name: 'domain-search', label: '域名搜索列表' },
  { code: 27, name: 'nis-servers', label: 'NIS 服务器' },
  { code: 56, name: 'ntp-server', label: 'NTP 服务器（RFC5908）' },
  { code: 64, name: 'aftr-name', label: 'AFTR（DS-Lite 隧道端点）' },
];
