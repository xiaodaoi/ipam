package coherence

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Controller 下发通道抽象：ExecController 走 unbound-control（§2.3）。
type Controller interface {
	// Add/Remove 均为单条 local_data 行；实现需幂等。
	Add(line string) error
	Remove(line string) error
}

var ErrUnboundUnavailable = fmt.Errorf("unbound-control unavailable")

// ExecController 真实下发；二进制缺失时返回 ErrUnboundUnavailable（对账器保留状态待重试）。
type ExecController struct{ Bin string }

// execTimeout 单次 unbound-control 调用预算（M6-052）：控制 socket 卡死时
// 不能让对账循环永久阻塞，否则联动 DNS 记录停止更新。
var execTimeout = 10 * time.Second

func (e ExecController) run(args ...string) error {
	bin := e.Bin
	if bin == "" {
		bin = "unbound-control"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ErrUnboundUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	// 超时后强制关闭残留管道，避免孙进程让 Wait 一直挂住（M6-052）。
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v: %s", bin, args, err, out)
	}
	return nil
}

func (e ExecController) Add(line string) error { return e.run("local_data", line) }
func (e ExecController) Remove(line string) error {
	// local_data_remove 按 name 移除整组 rrset；行内 name 为首字段
	name := strings.Fields(line)[0]
	return e.run("local_data_remove", name)
}

// Reconciler 差分对账器：Sync 幂等可重跑；失败保留 applied 以便下轮重试（K9）。
type Reconciler struct {
	ctl     Controller
	zone    string
	mu      sync.Mutex
	applied map[string]string

	// 全量重放（M6-052）：unbound 进程重启后其 local_data 清空，而本进程的
	// applied 仍认为已下发。每 replayEvery 清空一次 applied，强制重放一遍，
	// 保证「只重启 unbound」也能在数分钟内自愈。
	lastReplay  time.Time
	replayEvery time.Duration
}

func NewReconciler(ctl Controller, zone string) *Reconciler {
	return &Reconciler{ctl: ctl, zone: zone, applied: map[string]string{},
		replayEvery: 5 * time.Minute}
}

// Sync 以 bindings 全量为目标态收敛 unbound local_data。
func (r *Reconciler) Sync(bindings []Binding) error {
	desired := map[string]string{}
	for _, b := range bindings {
		for k, line := range DesiredRRs(b, r.zone) {
			desired[k] = line
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.replayEvery > 0 && time.Since(r.lastReplay) >= r.replayEvery {
		r.applied = map[string]string{}
		r.lastReplay = time.Now()
	}
	adds, dels := diff(r.applied, desired)

	// local_data_remove 按 name 删除整组 rrset：同一 FQDN 的 A/AAAA 会产生
	// 多条删除项，必须按 name 去重，否则第二次删除不存在的 name 会报错并
	// 中止后续 add（M6-052）。
	deleted := map[string]bool{}
	var firstErr error
	for _, d := range dels {
		name := strings.Fields(d)[0]
		if deleted[name] {
			delete(r.applied, keyOf(d))
			continue
		}
		deleted[name] = true
		if err := r.ctl.Remove(d); err != nil {
			// 删除失败不阻断 add：下轮重放会再次尝试，避免一条陈旧记录让
			// 整份联动 DNS 永久停更。
			if firstErr == nil {
				firstErr = err
			}
			logErr("unbound local_data_remove %s: %v", name, err)
			continue
		}
		delete(r.applied, keyOf(d))
	}
	for _, a := range adds {
		if err := r.ctl.Add(a); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logErr("unbound local_data %s: %v", a, err)
			continue
		}
		r.applied[keyOf(a)] = a
	}
	return firstErr
}

func keyOf(line string) string {
	f := strings.Fields(line)
	if len(f) < 4 {
		return line
	}
	return f[0] + "|" + f[3] // name|TYPE
}

// Run 周期对账直至 ctx 取消；all 注入台账全量读取。
func (r *Reconciler) Run(ctx context.Context, interval time.Duration, all func() []Binding) {
	t := time.NewTicker(interval)
	defer t.Stop()
	syncOnce := func() {
		if err := r.Sync(all()); err != nil {
			logErr("reconcile deferred: %v", err)
		}
	}
	syncOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			syncOnce()
		}
	}
}
