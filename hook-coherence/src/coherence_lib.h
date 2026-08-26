// coherence_lib.h — 薄 hook 核心逻辑（零外部依赖，K4：协议/策略全在 Go 侧）
#pragma once

#include <string>
#include <vector>

namespace ipam::coherence {

struct Binding {
  std::string mac;
  std::string ipv4;
  std::string ipv6;
  std::string template_id;
  std::string hostname;
};

// 归一化任意常见 MAC 书写(冒号/横线/点/裸12位, 大小写)为小写冒号格式；
// 无法安全归一化时返回空串。
std::string NormalizeMac(const std::string& raw);

// 解析快照行协议 v2：
//   "# ..." 注释行跳过；字段以 '|' 分隔，恰 5 段；坏行静默跳过（降级通道宁缺勿错）。
std::vector<Binding> ParseSnapshot(const std::string& text);

// 按 MAC 精确匹配（输入先经 NormalizeMac）。
const Binding* FindBinding(const std::vector<Binding>& bindings,
                           const std::string& macRaw);

}  // namespace ipam::coherence

// 解析 DHCPv6 option 79（RFC 6939 客户端链路层地址）负载。
// 格式：2 字节硬件类型(大端) + N 字节链路层地址；以太网 htype=1 时取后 6 字节。
// 返回归一化小写冒号 MAC；非以太网/长度非法返回空串。
std::string ParseOption79(const std::string& payload);

// 组合便捷函数：option79 优先，失败回退 raw MAC 归一化。
std::string ResolveClientMac(const std::string& opt79Payload,
                             const std::string& rawMacFallback);
