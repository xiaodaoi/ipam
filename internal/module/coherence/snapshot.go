package coherence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteSnapshotAtomic 将台账全量快照写入 path（tmp+rename 原子替换，§2.1 降级策略）。
// 格式：JSON 数组，C++ hook 侧只读。
// 行协议 v2：mac|ipv4|ipv6|template_id|hostname（C++ 侧零依赖解析）
func SnapshotLines(bindings []Binding) []byte {
	var sb strings.Builder
	sb.WriteString("# ipam bindings.snapshot v2\n")
	for _, b := range bindings {
		sb.WriteString(strings.Join([]string{b.MAC, b.IPv4, b.IPv6, b.TemplateID, b.Hostname}, "|"))
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

func WriteSnapshotAtomic(path string, bindings []Binding) error {
	data := SnapshotLines(bindings)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp." + time.Now().Format("150405.000000000")
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// StartSnapshotLoop 每 interval 全量刷新快照；返回停止函数。
// all 由调用方注入（MemStore.All 或 PG 查询），保持本函数无状态可测。
func StartSnapshotLoop(path string, interval time.Duration, all func() []Binding) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		flush := func() {
			if err := WriteSnapshotAtomic(path, all()); err != nil {
				logErr("snapshot: %v", err)
			}
		}
		flush()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				flush()
			}
		}
	}()
	return func() { close(done) }
}

// 统一日志出口：daemon main 注入 logger 前的兜底。
var logErr = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "coherence: "+format+"\n", args...)
}

// SetLogger 供 main 覆盖日志出口。
func SetLogger(f func(format string, args ...any)) { logErr = f }

// All 供快照/对账循环读取全量（实现于 MemStore）。
type AllProvider interface{ All() []Binding }
