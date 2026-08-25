# cmd/ — Go 程序入口

- `control-plane/`：管理/API/UI embed 服务（:8443），前端产物置于 `webui/dist`（go:embed）
- `coherence-daemon/`：v4v6 联动守护进程（gRPC over UDS，无网络端口）

进程角色见架构文档 §1 进程清单、§14。
