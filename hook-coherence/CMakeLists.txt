cmake_minimum_required(VERSION 3.16)
project(ipam_coherence_hook CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# 核心库：零外部依赖，CI 必编译必测
add_library(coherence_core src/coherence_lib.cc)
target_include_directories(coherence_core PUBLIC src)

enable_testing()
add_executable(test_coherence_lib tests/test_coherence_lib.cc)
target_link_libraries(test_coherence_lib PRIVATE coherence_core)
add_test(NAME coherence_lib COMMAND test_coherence_lib)

# Kea 胶水层：仅提供 KEA_HEADERS_DIR 时编译（PoC 真机环境，M1-005）
if(DEFINED KEA_HEADERS_DIR)
  add_library(coherence_hook MODULE src/hook_entry.cc)
  target_include_directories(coherence_hook PRIVATE ${KEA_HEADERS_DIR} src ../proto/gen/cpp)
  target_compile_definitions(coherence_hook PRIVATE IPAM_HAVE_KEA)
endif()

option(FUZZ "构建 libFuzzer 模糊测试" OFF)
if(FUZZ AND CMAKE_CXX_COMPILER_ID MATCHES "Clang")
  add_executable(fuzz_snapshot tests/fuzz_snapshot.cc)
  target_compile_options(fuzz_snapshot PRIVATE -fsanitize=fuzzer)
  target_link_options(fuzz_snapshot PRIVATE -fsanitize=fuzzer)
  target_link_libraries(fuzz_snapshot PRIVATE coherence_core)
endif()
