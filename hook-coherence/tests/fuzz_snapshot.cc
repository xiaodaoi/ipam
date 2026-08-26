// fuzz_snapshot — 快照解析器模糊测试（K4 CI 强制项）
// 构建：cmake -DFUZZ=ON && cmake --build . && ./fuzz_snapshot -runs=100000
#include <cstdint>
#include <cstddef>

#include "coherence_lib.h"

extern "C" int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
  if (size > 1 << 20) return 0;  // 上限防 OOM
  std::string text(reinterpret_cast<const char*>(data), size);
  auto bindings = ipam::coherence::ParseSnapshot(text);
  if (!bindings.empty()) {
    (void)ipam::coherence::FindBinding(bindings, text.substr(0, 32));
  }
  return 0;
}
