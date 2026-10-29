package hostmetrics

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCollector_ParseAndCPUUsage(t *testing.T) {
	dir := t.TempDir()
	proc := filepath.Join(dir, "proc")
	root := filepath.Join(dir, "root")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	stat := filepath.Join(proc, "stat")
	// 首次采样：total=1000, idle=800
	write(t, stat, "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 50 0 50 400 0 0 0 0 0 0\ncpu1 50 0 50 400 0 0 0 0 0 0\n")
	write(t, filepath.Join(proc, "loadavg"), "0.50 0.40 0.30 1/100 1234\n")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       1000 kB\nMemFree:         100 kB\nMemAvailable:    400 kB\n")
	write(t, filepath.Join(proc, "uptime"), "12345.67 8900.00\n")
	// 宿主 PID1 挂载表（优先读取）；proc/mounts 为 self 不使用
	if err := os.MkdirAll(filepath.Join(proc, "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(proc, "1", "mounts"),
		"/dev/sda1 / ext4 rw,relatime 0 0\n"+
			"/dev/sdb1 /data xfs rw 0 0\n"+
			"tmpfs /run tmpfs rw 0 0\n"+
			"/dev/loop0 /snap/core squashfs ro 0 0\n")
	write(t, filepath.Join(proc, "mounts"), "tmpfs /run tmpfs rw 0 0\n")

	c := New(proc, "", root)

	m1, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if m1.CPU.Cores != 2 {
		t.Fatalf("cores = %d, want 2", m1.CPU.Cores)
	}
	if m1.CPU.UsagePct != 0 {
		t.Fatalf("首次采样 usage = %v, want 0", m1.CPU.UsagePct)
	}
	if m1.CPU.Load1 != 0.5 || m1.CPU.Load15 != 0.3 {
		t.Fatalf("loadavg 解析异常: %+v", m1.CPU)
	}
	if m1.Memory.TotalBytes != 1000*1024 || m1.Memory.AvailableBytes != 400*1024 {
		t.Fatalf("meminfo 解析异常: %+v", m1.Memory)
	}
	if m1.Memory.UsedBytes != 600*1024 {
		t.Fatalf("mem used = %d, want %d", m1.Memory.UsedBytes, 600*1024)
	}
	if m1.UptimeSec != 12345.67 {
		t.Fatalf("uptime = %v", m1.UptimeSec)
	}
	// tmpfs 与非 /dev 源被过滤，仅 root 与 /data 两个块设备文件系统，按挂载点排序
	if len(m1.Disks) != 2 {
		t.Fatalf("disks = %d, want 2 (%+v)", len(m1.Disks), m1.Disks)
	}
	if m1.Disks[0].Mount != "/" || m1.Disks[1].Mount != "/data" {
		t.Fatalf("挂载点排序异常: %+v", m1.Disks)
	}
	for _, d := range m1.Disks {
		if d.TotalBytes == 0 || d.UsedPct < 0 || d.UsedPct > 100 {
			t.Fatalf("磁盘数值异常: %+v", d)
		}
	}

	// 二次采样：dTotal=100, dIdle=50 → usage 50%
	write(t, stat, "cpu  150 0 100 850 0 0 0 0 0 0\ncpu0 75 0 50 425 0 0 0 0 0 0\ncpu1 75 0 50 425 0 0 0 0 0 0\n")
	m2, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect #2: %v", err)
	}
	if m2.CPU.UsagePct < 49 || m2.CPU.UsagePct > 51 {
		t.Fatalf("usage = %v, want ≈50", m2.CPU.UsagePct)
	}
}

func TestCollector_MissingProc(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "nonexistent"), "", "")
	if _, err := c.Collect(); err == nil {
		t.Fatal("缺失 /proc/stat 应返回 error")
	}
}
