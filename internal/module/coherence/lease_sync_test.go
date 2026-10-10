package coherence

import (
	"testing"
	"time"
)

func TestNextBackoff_翻倍封顶与复位(t *testing.T) {
	base := 30 * time.Second
	cases := []struct {
		name      string
		failCount int
		want      time.Duration
	}{
		{"成功复位回基准", 0, 30 * time.Second},
		{"第 1 次失败翻倍", 1, time.Minute},
		{"第 2 次再翻倍", 2, 2 * time.Minute},
		{"第 3 次 4 分钟", 3, 4 * time.Minute},
		{"8 分钟超上限封顶 5 分钟", 4, 5 * time.Minute},
		{"持续失败维持上限", 5, 5 * time.Minute},
		{"大失败计数不溢出", 100, 5 * time.Minute},
	}
	for _, c := range cases {
		if got := nextBackoff(c.failCount, base); got != c.want {
			t.Fatalf("%s: nextBackoff(%d)=%s want %s", c.name, c.failCount, got, c.want)
		}
	}
}
