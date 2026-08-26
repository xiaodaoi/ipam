package ipam

import (
	"fmt"
)

func fmt1(n int) string { return fmt.Sprintf("%d", n) }

// nullStr 空串转 SQL NULL。
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
