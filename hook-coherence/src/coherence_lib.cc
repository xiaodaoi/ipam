#include "coherence_lib.h"

#include <ctype.h>

#include <array>
#include <cstddef>

namespace ipam::coherence {
namespace {

bool IsHex(char c) {
  return isdigit(static_cast<unsigned char>(c)) != 0 ||
         (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
}

}  // namespace

std::string NormalizeMac(const std::string& raw) {
  std::array<char, 12> hex{};
  std::size_t n = 0;
  for (char c : raw) {
    if (IsHex(c)) {
      if (n >= hex.size()) return "";
      hex[n++] = static_cast<char>(tolower(static_cast<unsigned char>(c)));
    } else if (c == ':' || c == '-' || c == '.') {
      continue;
    } else {
      return "";
    }
  }
  if (n != hex.size()) return "";

  std::string out;
  out.reserve(17);
  for (std::size_t i = 0; i < n; ++i) {
    if (i > 0 && i % 2 == 0) out.push_back(':');
    out.push_back(hex[i]);
  }
  return out;
}

std::vector<Binding> ParseSnapshot(const std::string& text) {
  std::vector<Binding> out;
  std::size_t pos = 0;
  while (pos <= text.size()) {
    std::size_t eol = text.find('\n', pos);
    if (eol == std::string::npos) eol = text.size();
    const std::string line = text.substr(pos, eol - pos);
    pos = eol + 1;

    if (line.empty() || line[0] == '#') continue;

    std::string fields[5];
    std::size_t idx = 0;
    for (char c : line) {
      if (c == '|') {
        if (++idx >= 5) break;
        continue;
      }
      fields[idx].push_back(c);
    }
    if (idx != 4) continue;  // 恰 5 段；坏行跳过

    Binding b;
    b.mac = NormalizeMac(fields[0]);
    b.ipv4 = fields[1];
    b.ipv6 = fields[2];
    b.template_id = fields[3];
    b.hostname = fields[4];
    if (!b.mac.empty()) out.push_back(std::move(b));
  }
  return out;
}

const Binding* FindBinding(const std::vector<Binding>& bindings,
                           const std::string& macRaw) {
  const std::string want = NormalizeMac(macRaw);
  if (want.empty()) return nullptr;
  for (const auto& b : bindings) {
    if (b.mac == want) return &b;
  }
  return nullptr;
}

}  // namespace ipam::coherence

std::string ParseOption79(const std::string& payload) {
  // RFC 6939：前 2 字节为硬件类型（网络序），以太网=1；其后为链路层地址
  if (payload.size() < 3) return "";
  const unsigned char htype =
      static_cast<unsigned char>(payload[1]);  // 高字节通常为 0
  if (htype != 0x01) return "";                // 仅支持以太网
  std::string mac;
  mac.reserve(17);
  for (int i = 2; i < 8 && i < static_cast<int>(payload.size()); ++i) {
    if (i > 2) mac.push_back(':');
    unsigned char c = static_cast<unsigned char>(payload[i]);
    char buf[3];
    snprintf(buf, sizeof(buf), "%02x", c);
    mac.append(buf);
  }
  return mac.size() == 17 ? mac : "";
}

std::string ResolveClientMac(const std::string& opt79Payload,
                             const std::string& rawMacFallback) {
  auto m = ParseOption79(opt79Payload);
  if (!m.empty()) return m;
  return NormalizeMac(rawMacFallback);
}
