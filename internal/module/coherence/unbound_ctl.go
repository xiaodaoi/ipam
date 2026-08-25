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

func (e ExecController) run(args ...string) error {
	bin := e.Bin
	if bin == "" {
		bin = "unbound-control"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ErrUnboundUnavailable
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
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
}

func NewReconciler(ctl Controller, zone string) *Reconciler {
	return &Reconciler{ctl: ctl, zone: zone, applied: map[string]string{}}
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
	adds, dels := diff(r.applied, desired)
	for _, d := range dels {
		if err := r.ctl.Remove(d); err != nil {
			return err
		}
		delete(r.applied, keyOf(d))
	}
	for _, a := range adds {
		if err := r.ctl.Add(a); err != nil {
			return err
		}
		r.applied[keyOf(a)] = a
	}
	return nil
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
