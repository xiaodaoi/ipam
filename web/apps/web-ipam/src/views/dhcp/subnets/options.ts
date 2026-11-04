// DHCP 标准选项预设（RFC2132 / RFC8415 常用项；name 为 Kea 标准选项名）。
import { $t } from '#/locales';

export interface DhcpOptionPreset {
  code: number;
  name: string;
  label: string; // 中文名 + 说明
}

// 预设文案随语言切换，调用时现取当前语言。
export function getDhcp4OptionPresets(): DhcpOptionPreset[] {
  return [
    { code: 2, name: 'time-offset', label: $t('dhcp.optionPresets.v4.timeOffset') },
    { code: 4, name: 'time-servers', label: $t('dhcp.optionPresets.v4.timeServers') },
    { code: 7, name: 'log-servers', label: $t('dhcp.optionPresets.v4.logServers') },
    { code: 15, name: 'domain-name', label: $t('dhcp.optionPresets.v4.domainName') },
    { code: 19, name: 'ip-forwarding', label: $t('dhcp.optionPresets.v4.ipForwarding') },
    { code: 28, name: 'broadcast-address', label: $t('dhcp.optionPresets.v4.broadcastAddress') },
    { code: 42, name: 'ntp-servers', label: $t('dhcp.optionPresets.v4.ntpServers') },
    { code: 44, name: 'netbios-name-servers', label: $t('dhcp.optionPresets.v4.netbiosNameServers') },
    { code: 66, name: 'tftp-server-name', label: $t('dhcp.optionPresets.v4.tftpServerName') },
    { code: 67, name: 'bootfile-name', label: $t('dhcp.optionPresets.v4.bootfileName') },
  ];
}

export function getDhcp6OptionPresets(): DhcpOptionPreset[] {
  return [
    { code: 7, name: 'preference', label: $t('dhcp.optionPresets.v6.preference') },
    { code: 21, name: 'sip-server-dns', label: $t('dhcp.optionPresets.v6.sipServerDns') },
    { code: 23, name: 'dns-servers', label: $t('dhcp.optionPresets.v6.dnsServers') },
    { code: 24, name: 'domain-search', label: $t('dhcp.optionPresets.v6.domainSearch') },
    { code: 27, name: 'nis-servers', label: $t('dhcp.optionPresets.v6.nisServers') },
    { code: 56, name: 'ntp-server', label: $t('dhcp.optionPresets.v6.ntpServer') },
    { code: 64, name: 'aftr-name', label: $t('dhcp.optionPresets.v6.aftrName') },
  ];
}
