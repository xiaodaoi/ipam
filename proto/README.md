# proto/ — hook ↔ daemon 共享契约

Coherence 服务 gRPC 定义：ResolveBinding（同步解析）、ReportLease（异步上报）。契约原文见架构文档 §2.1；由 Go 与 C++ 两侧共同引用生成。
