// hook_entry.cc — Kea 胶水层（仅生产构建参与编译：需 KEA_HEADERS_DIR）。
// 设计约束（K4）：报文提取与地址注入在此，决策/协议逻辑全在 Go daemon；
// UDS gRPC 不可达时降级读 bindings.snapshot（§2.1）。
#ifdef IPAM_HAVE_KEA

#include <hooks/callout_handle.h>
#include <hooks/library_handles.h>
#include <dhcp/pkt6.h>
#include <dhcp/hwaddr.h>

#include "coherence_lib.h"

#include <string>

namespace {

using namespace isc::hooks;
using namespace isc::dhcp;

const char* kSnapshotPath = "/run/ipam/bindings.snapshot";

std::vector<ipam::coherence::Binding> LoadSnapshot() {
  // 读取失败返回空集：宁缺勿错，等待下轮快照刷新
  FILE* f = fopen(kSnapshotPath, "rb");
  if (!f) return {};
  std::string text;
  char buf[4096];
  size_t n;
  while ((n = fread(buf, 1, sizeof(buf), f)) > 0) text.append(buf, n);
  fclose(f);
  return ipam::coherence::ParseSnapshot(text);
}

std::string MacFromDuid(const std::string& duid) {
  // DUID-LLT/DUID-EN 提取 MAC 的规则引擎在 Go 侧；hook 仅透传 duid，
  // 快照匹配按 mac 键进行，duid 匹配由 daemon 侧完成（此处恒空实现占位）。
  (void)duid;
  return "";
}

}  // namespace

extern "C" {

// pkt6_receive：查询绑定并把结果挂到 handle argument（lease6_select 注入用）
int pkt6_receive(CalloutHandle& handle) {
  Pkt6Ptr pkt;
  handle.getArgument("query6", pkt);
  if (!pkt) return 0;

  std::string mac;
  auto hw = pkt->getMAC(HWADDR_SOURCE_ANY);
  if (hw) mac = hw->toText(false);

  auto bindings = LoadSnapshot();
  const auto* b = ipam::coherence::FindBinding(bindings, mac);
  if (!b) {
    b = ipam::coherence::FindBinding(bindings, MacFromDuid(""));
  }
  if (b && !b->ipv6.empty()) {
    handle.setArgument("coherence_ipv6", b->ipv6);
    handle.setArgument("coherence_hit", true);
  }
  return 0;
}

// lease6_select：命中则覆盖 IA_NA/IA_TA 地址
int lease6_select(CalloutHandle& handle) {
  bool hit = false;
  try { handle.getArgument("coherence_hit", hit); } catch (...) { return 0; }
  if (!hit) return 0;

  std::string ipv6;
  handle.getArgument("coherence_ipv6", ipv6);
  Lease6Ptr lease;
  handle.getArgument("lease6", lease);
  if (lease) {
    lease->addr_ = isc::asiolink::IOAddress(ipv6);
  }
  return 0;
}

int load(LibraryHandle&, const std::vector<std::string>&) { return 0; }
int unload() { return 0; }

}  // extern "C"

#endif  // IPAM_HAVE_KEA
