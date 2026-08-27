package logquery

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cursor 编码："{tsUnixMilli}:{client_mac}:{domain}"。
// client_mac 为 12 位小写 hex、domain 为 DNS 名（均不含冒号），可直接拼装；
// 排序语义 (ts DESC, client_mac DESC, domain DESC) 与 CH/内存两实现共享。
func EncodeCursor(ts time.Time, mac, domain string) string {
	return fmt.Sprintf("%d:%s:%s", ts.UnixMilli(), mac, domain)
}

// ParseCursor 解析游标；非法输入返回零值（service 层先校验格式）。
func ParseCursor(s string) (ts time.Time, mac, domain string) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return time.Time{}, "", ""
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", ""
	}
	return time.UnixMilli(ms).UTC(), parts[1], parts[2]
}

// beforeCursor 判断行是否严格位于游标之后（DESC 序：ts, client_mac, domain）。
// 返回 true = 该行在游标之后，应保留。
func beforeCursor(row LogRow, cts time.Time, cmac, cdomain string) bool {
	rt := row.TS.UTC().UnixMilli()
	ct := cts.UTC().UnixMilli()
	if rt != ct {
		return rt < ct
	}
	if row.ClientMAC != cmac {
		return row.ClientMAC < cmac
	}
	return row.Domain < cdomain
}
