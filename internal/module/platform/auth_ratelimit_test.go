package platform

import (
	"testing"
	"time"
)

// Given 每 IP 5 次/分钟窗口，When 窗口内第 6 次尝试，Then 拒绝且给出 retryAfter；窗口过后放行。
func TestLoginRateLimiter_窗口限流与冷却恢复(t *testing.T) {
	cur := time.Now()
	l := newLoginRateLimiter(func() time.Time { return cur })
	for i := 0; i < 5; i++ {
		if ok, _ := l.allow("1.2.3.4"); !ok {
			t.Fatalf("第 %d 次尝试应放行", i+1)
		}
	}
	ok, retry := l.allow("1.2.3.4")
	if ok {
		t.Fatal("第 6 次尝试应被限流")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retryAfter 应在窗口内: %v", retry)
	}
	cur = cur.Add(61 * time.Second)
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Fatal("窗口过后应放行（合法冷却重试不受限）")
	}
}

// Given IP-A 已超限，When IP-B 首次尝试，Then B 放行且 A 仍被拒（按 IP 独立计数）。
func TestLoginRateLimiter_按IP隔离(t *testing.T) {
	cur := time.Now()
	l := newLoginRateLimiter(func() time.Time { return cur })
	for i := 0; i < 5; i++ {
		l.allow("1.1.1.1")
	}
	if ok, _ := l.allow("2.2.2.2"); !ok {
		t.Fatal("其他 IP 不应受限")
	}
	if ok, _ := l.allow("1.1.1.1"); ok {
		t.Fatal("已超限 IP 仍应被拒")
	}
}

// Given 两个空闲桶，When 一个窗口后新 IP 访问，Then 惰性周期清扫驱逐空闲桶。
func TestLoginRateLimiter_周期驱逐空闲桶(t *testing.T) {
	cur := time.Now()
	l := newLoginRateLimiter(func() time.Time { return cur })
	l.allow("1.1.1.1")
	l.allow("2.2.2.2")
	cur = cur.Add(2 * time.Minute)
	l.allow("3.3.3.3")
	if len(l.buckets) != 1 {
		t.Fatalf("空闲桶应被周期驱逐: %d", len(l.buckets))
	}
}
