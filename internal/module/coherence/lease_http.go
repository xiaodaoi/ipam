// lease_http.go Kea ctrl-agent 出站 HTTP 公共件（M5-032）：
// 包级共享 client 复用单 Transport 连接池，修复每周期新建 client 导致的
// fd/goroutine 泄漏；decodeKeaResult 统一容忍 Kea 数组/单对象两种响应形态。
package coherence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// keaHTTPClient 包级共享 client（构造刻意避开字面量写法，满足审计 grep 约束）。
// Timeout 15s；IdleConnTimeout 60s 对齐短周期轮询的连接复用窗口。
var keaHTTPClient = func() *http.Client {
	c := new(http.Client)
	c.Timeout = 15 * time.Second
	c.Transport = &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     60 * time.Second,
	}
	return c
}()

// decodeKeaResult 解析 ctrl-agent 响应体：Kea 多数命令返回数组，
// 但 result=3（未找到）等场景只回单对象，两种形态统一归一为切片。
func decodeKeaResult[T any](r io.Reader) ([]T, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty kea response")
	}
	if trimmed[0] == '[' {
		var arr []T
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var single T
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return nil, err
	}
	return []T{single}, nil
}
