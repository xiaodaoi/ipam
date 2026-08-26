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
