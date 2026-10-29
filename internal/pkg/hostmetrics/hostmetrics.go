// Package hostmetrics 采集宿主主机 CPU/内存/磁盘使用率。
//
// 控制面运行在容器内，默认只能看到容器自身 cgroup；本包读取的是经只读挂载暴露的
// 宿主 /proc（IPAM_HOST_PROC）、/sys（IPAM_HOST_SYS）与宿主根或数据盘（IPAM_HOST_ROOT），
// 因此返回的是「宿主整机」指标。无挂载（如宿主直跑）时退化为读本机 /proc、/。
package hostmetrics

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// CPU 使用率与负载。
type CPU struct {
	UsagePct float64 // 两次采样差值；首次调用为 0
	Cores    int
	Load1    float64
	Load5    float64
	Load15   float64
}

// Memory 内存（字节）。
type Memory struct {
	TotalBytes     uint64
	UsedBytes      uint64
	AvailableBytes uint64
	UsedPct        float64
}

// Disk 单文件系统使用率（字节）。
type Disk struct {
	Mount          string
	Device         string
	FSType         string
	TotalBytes     uint64
	UsedBytes      uint64
	AvailableBytes uint64
	UsedPct        float64
}

// Metrics 一次采样快照。
type Metrics struct {
	TS        time.Time
	UptimeSec float64
	CPU       CPU
	Memory    Memory
	Disks     []Disk
}

// Collector 有状态采集器（CPU 使用率依赖上一次 /proc/stat 采样）。
type Collector struct {
	procRoot string
	sysRoot  string
	hostRoot string

	mu        sync.Mutex
	prevTotal uint64
	prevIdle  uint64
}

// New 构造采集器；空串回落宿主直读路径（/proc、/sys、/）。
func New(procRoot, sysRoot, hostRoot string) *Collector {
	if procRoot == "" {
		procRoot = "/proc"
	}
	if sysRoot == "" {
		sysRoot = "/sys"
	}
	if hostRoot == "" {
		hostRoot = "/"
	}
	return &Collector{procRoot: procRoot, sysRoot: sysRoot, hostRoot: hostRoot}
}

// Collect 采集一次快照。CPU stat 读取失败视为致命（返回 error），其余字段软失败（零值）。
func (c *Collector) Collect() (Metrics, error) {
	m := Metrics{TS: time.Now().UTC()}
	if err := c.collectCPU(&m); err != nil {
		return m, err
	}
	c.collectMemory(&m)
	c.collectUptime(&m)
	m.Disks = c.collectDisks()
	return m, nil
}

// collectCPU 解析 /proc/stat 首行聚合 + /proc/loadavg 负载，并按两次采样差值算使用率。
func (c *Collector) collectCPU(m *Metrics) error {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "stat"))
	if err != nil {
		return err
	}
	var total, idle uint64
	cores := 0
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "cpu" { // cpu0、cpu1… → 计核数
			cores++
			continue
		}
		// user nice system idle iowait irq softirq steal [guest guest_nice]
		var vals []uint64
		for _, f := range fields[1:] {
			v, perr := strconv.ParseUint(f, 10, 64)
			if perr != nil {
				break
			}
			vals = append(vals, v)
		}
		for i, v := range vals {
			if i >= 8 {
				break
			}
			total += v
		}
		if len(vals) > 4 {
			idle = vals[3] + vals[4]
		} else if len(vals) > 3 {
			idle = vals[3]
		}
	}
	m.CPU.Cores = cores

	c.mu.Lock()
	if c.prevTotal != 0 && total > c.prevTotal {
		dTotal := total - c.prevTotal
		dIdle := idle - c.prevIdle
		if dTotal > 0 {
			m.CPU.UsagePct = (1 - float64(dIdle)/float64(dTotal)) * 100
			if m.CPU.UsagePct < 0 {
				m.CPU.UsagePct = 0
			}
		}
	}
	c.prevTotal, c.prevIdle = total, idle
	c.mu.Unlock()

	if lb, lerr := os.ReadFile(filepath.Join(c.procRoot, "loadavg")); lerr == nil {
		if f := strings.Fields(string(lb)); len(f) >= 3 {
			m.CPU.Load1, _ = strconv.ParseFloat(f[0], 64)
			m.CPU.Load5, _ = strconv.ParseFloat(f[1], 64)
			m.CPU.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	return nil
}

// collectMemory 解析 /proc/meminfo（kB → 字节）。
func (c *Collector) collectMemory(m *Metrics) {
	b, err := os.ReadFile(filepath.Join(c.procRoot, "meminfo"))
	if err != nil {
		return
	}
	vals := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		if v, perr := strconv.ParseUint(parts[1], 10, 64); perr == nil {
			vals[key] = v * 1024
		}
	}
	total := vals["MemTotal"]
	avail := vals["MemAvailable"]
	if avail == 0 {
		avail = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	m.Memory.TotalBytes = total
	m.Memory.AvailableBytes = avail
	if total > avail {
		m.Memory.UsedBytes = total - avail
	}
	if total > 0 {
		m.Memory.UsedPct = float64(m.Memory.UsedBytes) / float64(total) * 100
	}
}

// collectUptime 读取 /proc/uptime 首字段。
func (c *Collector) collectUptime(m *Metrics) {
	b, err := os.ReadFile(filepath.Join(c.procRoot, "uptime"))
	if err != nil {
		return
	}
	if f := strings.Fields(string(b)); len(f) >= 1 {
		m.UptimeSec, _ = strconv.ParseFloat(f[0], 64)
	}
}

// collectDisks 枚举块设备文件系统并 statfs（经 hostRoot 拼接宿主路径）。
// 宿主挂载表取自 <procRoot>/1/mounts（宿主 PID1 的挂载命名空间；<procRoot>/mounts 实为
// /proc/self/mounts 会返回「容器自身」挂载，故不可用），无 1/mounts 时回退 self。
// 过滤伪文件系统（非 /dev/ 源）与 squashfs（snap 回环），按设备去重。
func (c *Collector) collectDisks() []Disk {
	mountsFile := filepath.Join(c.procRoot, "1", "mounts")
	if _, err := os.Stat(mountsFile); err != nil {
		mountsFile = filepath.Join(c.procRoot, "mounts")
	}
	b, err := os.ReadFile(mountsFile)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	disks := make([]Disk, 0, 4)
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		dev, mp, fstype := f[0], f[1], f[2]
		if !strings.HasPrefix(dev, "/dev/") || fstype == "squashfs" || seen[dev] {
			continue
		}
		var st syscall.Statfs_t
		if serr := syscall.Statfs(filepath.Join(c.hostRoot, mp), &st); serr != nil {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		if total == 0 {
			continue
		}
		avail := st.Bavail * bsize
		used := total - avail
		seen[dev] = true
		disks = append(disks, Disk{
			Mount:          mp,
			Device:         dev,
			FSType:         fstype,
			TotalBytes:     total,
			UsedBytes:      used,
			AvailableBytes: avail,
			UsedPct:        float64(used) / float64(total) * 100,
		})
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Mount < disks[j].Mount })
	return disks
}
