package logexport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderSinksAndSampling(t *testing.T) {
	base := `[sinks.clickhouse]
type = "clickhouse"
inputs = ["normalize"]
`
	out := Render(base, Settings{
		SyslogEnabled: true, SyslogAddress: "siem:514", SyslogMode: "udp",
		KafkaEnabled: true, KafkaBrokers: "kafka:9092", KafkaTopic: "ipam",
		WebhookEnabled: true, WebhookURL: "https://siem/hook",
		SamplingMode: "sample", SampleRate: 20,
	})
	for _, want := range []string{
		`inputs = ["log_out"]`,
		`[transforms.log_out]`,
		`type = "sample"`,
		`rate = 20`,
		`[sinks.siem_syslog]`,
		`mode = "udp"`,
		`encoding.codec = "syslog"`,
		`[sinks.siem_kafka]`,
		`topic = "ipam"`,
		`[sinks.siem_webhook]`,
		`uri = "https://siem/hook"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestRenderErrorsMode(t *testing.T) {
	out := Render(`[sinks.clickhouse]
inputs = ["normalize"]
`, Settings{SamplingMode: "errors", SampleRate: 10})
	if !strings.Contains(out, `type = "filter"`) || !strings.Contains(out, `.severity != "INFO"`) {
		t.Fatalf("errors filter missing:\n%s", out)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		s   Settings
		err bool
	}{
		{Settings{SamplingMode: "all", SampleRate: 10, SyslogMode: "tcp"}, false},
		{Settings{SyslogEnabled: true, SyslogAddress: "bad", SamplingMode: "all", SampleRate: 10}, true},
		{Settings{KafkaEnabled: true, SamplingMode: "all", SampleRate: 10}, true},
		{Settings{WebhookEnabled: true, WebhookURL: "ftp://x", SamplingMode: "all", SampleRate: 10}, true},
		{Settings{SamplingMode: "sample", SampleRate: 0}, true},
	}
	for i, c := range cases {
		err := Validate(c.s)
		if (err != nil) != c.err {
			t.Fatalf("case %d: err=%v", i, err)
		}
	}
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	tpl := filepath.Join(dir, "vector.toml")
	out := filepath.Join(dir, "out.toml")
	if err := os.WriteFile(tpl, []byte("[sinks.clickhouse]\ninputs = [\"normalize\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewService(NewMemRepo(), tpl, out), out
}

func TestAutoSamplingOverrideAndRestore(t *testing.T) {
	ctx := context.Background()
	svc, out := newTestService(t)
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); !strings.Contains(got, `type = "sample"`) || !strings.Contains(got, "rate = 1") {
		t.Fatalf("默认 all 应渲染 sample rate=1:\n%s", got)
	}
	if err := svc.SetAutoSampling(ctx, "errors"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); !strings.Contains(got, `type = "filter"`) {
		t.Fatalf("降级应渲染仅异常:\n%s", got)
	}
	st := svc.Status()
	if !st.Degraded || st.EffectiveSamplingMode != "errors" {
		t.Fatalf("状态错误: %+v", st)
	}
	// 降级期间保存用户策略：DB 保留用户值，但渲染仍使用覆盖模式。
	if _, err := svc.Save(ctx, Settings{SamplingMode: "all", SampleRate: 10, SyslogMode: "tcp"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); !strings.Contains(got, `type = "filter"`) {
		t.Fatalf("保存后降级应保持: \n%s", got)
	}
	saved, _ := svc.Get(ctx)
	if saved.SamplingMode != "all" {
		t.Fatalf("用户策略不应被覆盖写入: %+v", saved)
	}
	if err := svc.SetAutoSampling(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); strings.Contains(got, `type = "filter"`) {
		t.Fatalf("恢复后不应保留仅异常:\n%s", got)
	}
	if st := svc.Status(); st.Degraded {
		t.Fatalf("恢复后状态应为正常: %+v", st)
	}
}

func TestWatermarkHysteresis(t *testing.T) {
	ctx := context.Background()
	svc, out := newTestService(t)
	pct := 95.0
	wm := NewWatermark(svc, func() (float64, error) { return pct, nil },
		WatermarkConfig{High: 90, Low: 85, Consecutive: 2, Interval: time.Millisecond})
	if d, _ := wm.Evaluate(ctx); d {
		t.Fatal("首次命中不应立即降级")
	}
	if d, _ := wm.Evaluate(ctx); !d {
		t.Fatal("连续命中应降级")
	}
	if got := readFile(t, out); !strings.Contains(got, `type = "filter"`) {
		t.Fatalf("应渲染仅异常:\n%s", got)
	}
	// 回差区间保持降级
	pct = 88
	if d, _ := wm.Evaluate(ctx); !d {
		t.Fatal("回差区间应保持降级")
	}
	// 低于低水位需连续命中才恢复
	pct = 80
	if d, _ := wm.Evaluate(ctx); !d {
		t.Fatal("首次低于低水位不应立即恢复")
	}
	if d, _ := wm.Evaluate(ctx); d {
		t.Fatal("连续低于低水位应恢复")
	}
	if got := readFile(t, out); strings.Contains(got, `type = "filter"`) {
		t.Fatalf("恢复后不应保留仅异常:\n%s", got)
	}
	if got := svc.Status().DiskUsedPercent; got != 80 {
		t.Fatalf("磁盘使用率未记录: %v", got)
	}
}

func TestWatermarkConfigFromEnv(t *testing.T) {
	cfg := WatermarkConfigFromEnv(func(k string) string {
		if k == "IPAM_LOG_DEGRADE_HIGH" {
			return "92"
		}
		return "200"
	})
	if cfg.High != 92 || cfg.Low != 85 {
		t.Fatalf("非法/缺省值回退错误: %+v", cfg)
	}
	cfg = WatermarkConfigFromEnv(func(k string) string {
		if k == "IPAM_LOG_DEGRADE_HIGH" {
			return "85"
		}
		return "90"
	})
	if cfg.Low >= cfg.High {
		t.Fatalf("低水位必须小于高水位: %+v", cfg)
	}
	cfg = WatermarkConfigFromEnv(func(k string) string {
		if k == "IPAM_LOG_DEGRADE_INTERVAL" {
			return "3s"
		}
		return ""
	})
	if cfg.Interval != 3*time.Second {
		t.Fatalf("间隔解析失败: %+v", cfg)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
