package platform

import (
	"os"
	"testing"
)

// TestMain 固定测试用初始口令（生产未设置 IPAM_POC_PASSWORD 时为随机口令）。
func TestMain(m *testing.M) {
	_ = os.Setenv("IPAM_POC_PASSWORD", "admin123")
	os.Exit(m.Run())
}
