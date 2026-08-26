// 测试：核心库行为基线（零外部依赖，ctest 驱动）
#include "coherence_lib.h"

#include <cassert>
#include <cstdio>
#include <string>

using namespace ipam::coherence;

static int failures = 0;
#define CHECK(cond)                                                    \
  do {                                                                 \
    if (!(cond)) {                                                     \
      ++failures;                                                      \
      std::printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond);      \
    }                                                                  \
  } while (0)

static void TestNormalizeMac() {
  CHECK(NormalizeMac("AA-BB-cc-DD-ee-FF") == "aa:bb:cc:dd:ee:ff");
  CHECK(NormalizeMac("aabb.ccdd.eeff") == "aa:bb:cc:dd:ee:ff");
  CHECK(NormalizeMac("AABBCCDDEEFF") == "aa:bb:cc:dd:ee:ff");
  CHECK(NormalizeMac("aa:bb:cc:dd:ee:ff") == "aa:bb:cc:dd:ee:ff");
  CHECK(NormalizeMac("").empty());
  CHECK(NormalizeMac("zz:bb").empty());
  CHECK(NormalizeMac("aa:bb:cc").empty());  // 残缺拒收
}

static void TestParseSnapshot() {
  const char* snap =
      "# ipam bindings.snapshot v2\n"
      "aa:bb:cc:dd:ee:01|10.61.172.10|2406::10:61:172:10|t-b|printer\n"
      "\n"
      "bad-line-no-pipe\n"
      "aa:bb:cc:dd:ee:02|10.0.0.5|2406::5||\n";

  auto bindings = ParseSnapshot(snap);
  CHECK(bindings.size() == 2);
  if (bindings.size() == 2) {
    CHECK(bindings[0].mac == "aa:bb:cc:dd:ee:01");
    CHECK(bindings[0].ipv6 == "2406::10:61:172:10");
    CHECK(bindings[1].hostname.empty());
    CHECK(bindings[1].template_id.empty());
  }
}

static void TestFindBinding_DegradedMatch() {
  auto bindings = ParseSnapshot(
      "aa:bb:cc:dd:ee:01|10.61.172.10|2406::10:61:172:10|t-b|\n");
  // 终端报文里的 MAC 大小写/分隔符不可控，归一化后必须命中
  const Binding* b = FindBinding(bindings, "AA-BB-CC-DD-EE-01");
  CHECK(b != nullptr);
  if (b) CHECK(b->ipv6 == "2406::10:61:172:10");

  CHECK(FindBinding(bindings, "11:22:33:44:55:66") == nullptr);
  CHECK(FindBinding(bindings, "") == nullptr);
}

int main() {
  TestNormalizeMac();
  TestParseSnapshot();
  TestFindBinding_DegradedMatch();
  if (failures) {
    std::printf("FAILED: %d check(s)\n", failures);
    return 1;
  }
  std::printf("ALL PASS\n");
  return 0;
}
