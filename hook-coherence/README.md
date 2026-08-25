# hook-coherence/ — 薄 C++ Kea hook（libcoherence.so）

报文级 MAC 提取与地址注入（pkt6_receive / lease6_select）。约束：**<800 行**、协议逻辑全在 Go 侧 daemon；CI 强制 fuzz（风险 K4）。

接口契约见架构文档 §2.1，双路径设计见 §4.2。
