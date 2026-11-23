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
endpoint = "http://clickhouse:8123"
`
	out := Render(base, Settings{
		SyslogEnabled: true, SyslogAddress: "siem:514", SyslogMode: "udp",
		KafkaEnabled: true, KafkaBrokers: "kafka:9092", KafkaTopic: "ipam",
		WebhookEnabled: true, WebhookURL: "https://siem/hook",
		SamplingMode: "sample", SampleRate: 20,
	}, 1)
	for _, want := range []string{
		`[transforms.ch_out]`,
		`[transforms.log_out]`,
		`type = "sample"`,
		`rate = 1`,
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
	// ClickHouse 必须接内部链路 ch_out，而不是外发链路 log_out。
	if !sectionHas(out, "[sinks.clickhouse]", "[transforms.ch_out]", `inputs = ["ch_out"]`) {
		t.Fatalf("clickhouse 未接入 ch_out:\n%s", out)
	}
}

func TestRenderExportErrorsKeepsIngestFull(t *testing.T) {
	base := `[sinks.clickhouse]
inputs = ["normalize"]
`
	out := Render(base, Settings{
		SamplingMode: "errors", WebhookEnabled: true, WebhookURL: "https://siem/hook",
	}, 1)
	// 内部入库仍为全量；errors 仅作用于 log_out 外发。
	if !sectionHas(out, "[transforms.ch_out]", "[transforms.log_out]", `rate = 1`) {
		t.Fatalf("外发仅异常不应影响内部入库:\n%s", out)
	}
	if !strings.Contains(out, `.severity != "INFO"`) {
		t.Fatalf("外发 errors 过滤器缺失:\n%s", out)
	}
	if !sectionHas(out, "[sinks.clickhouse]", "[transforms.ch_out]", `inputs = ["ch_out"]`) {
		t.Fatalf("clickhouse 未接入 ch_out:\n%s", out)
	}
}

func TestRenderDegradedSamplesIngest(t *testing.T) {
	base := `[sinks.clickhouse]
inputs = ["normalize"]
`
	out := Render(base, Settings{
		SamplingMode: "errors", SyslogEnabled: true, SyslogAddress: "siem:514",
	}, 10)
	if !sectionHas(out, "[transforms.ch_out]", "[transforms.log_out]", `rate = 10`) {
		t.Fatalf("降级期间内部入库应按 1/10 抽样:\n%s", out)
	}
	if !strings.Contains(out, `.severity != "INFO"`) {
		t.Fatalf("降级期间外发应为仅异常:\n%s", out)
	}
}

// sectionHas 判断 out 在 [start, end) 段落内是否含 want（end 为空表示到文末）。
func sectionHas(out, start, end, want string) bool {
	i := strings.Index(out, start)
	if i < 0 {
		return false
	}
	seg := out[i:]
	if end != "" {
		if j := strings.Index(seg, end); j >= 0 {
			seg = seg[:j]
		}
	}
	return strings.Contains(seg, want)
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

func newTestServiceWith(t *testing.T, st Settings) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	tpl := filepath.Join(dir, "vector.toml")
	out := filepath.Join(dir, "out.toml")
	if err := os.WriteFile(tpl, []byte("[sinks.clickhouse]\ninputs = [\"normalize\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := NewMemRepo()
	repo.s = st
	return NewService(repo, tpl, out), out
}

func TestAutoSamplingOverrideAndRestore(t *testing.T) {
	ctx := context.Background()
	svc, out := newTestServiceWith(t, Settings{
		SyslogMode: "tcp", SamplingMode: "all", SampleRate: 10,
		WebhookEnabled: true, WebhookURL: "https://siem/hook",
	})
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); !sectionHas(got, "[transforms.ch_out]", "[transforms.log_out]", "rate = 1") {
		t.Fatalf("默认 all 应全量入库 rate=1:\n%s", got)
	}
	if err := svc.SetAutoSampling(ctx, "errors"); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, out)
	if !sectionHas(got, "[transforms.ch_out]", "[transforms.log_out]", "rate = 10") {
		t.Fatalf("降级应保持内部 1/10 抽样:\n%s", got)
	}
	if !strings.Contains(got, `.severity != "INFO"`) {
		t.Fatalf("降级应渲染外发仅异常:\n%s", got)
	}
	st := svc.Status()
	if !st.Degraded || st.EffectiveSamplingMode != "errors" || st.EffectiveIngestSampleRate != 10 {
		t.Fatalf("状态错误: %+v", st)
	}
	// 降级期间保存用户策略：DB 保留用户值，但渲染仍使用覆盖模式。
	if _, err := svc.Save(ctx, Settings{SamplingMode: "all", SampleRate: 10, SyslogMode: "tcp",
		WebhookEnabled: true, WebhookURL: "https://siem/hook"}); err != nil {
		t.Fatal(err)
	}
	got = readFile(t, out)
	if !strings.Contains(got, `.severity != "INFO"`) || !sectionHas(got, "[transforms.ch_out]", "[transforms.log_out]", "rate = 10") {
		t.Fatalf("保存后降级应保持: \n%s", got)
	}
	saved, _ := svc.Get(ctx)
	if saved.SamplingMode != "all" {
		t.Fatalf("用户策略不应被覆盖写入: %+v", saved)
	}
	if err := svc.SetAutoSampling(ctx, ""); err != nil {
		t.Fatal(err)
	}
	got = readFile(t, out)
	if strings.Contains(got, `.severity != "INFO"`) {
		t.Fatalf("恢复后不应保留外发仅异常:\n%s", got)
	}
	if !sectionHas(got, "[transforms.ch_out]", "[transforms.log_out]", "rate = 1") {
		t.Fatalf("恢复后内部入库应回到全量:\n%s", got)
	}
	if st := svc.Status(); st.Degraded || st.EffectiveIngestSampleRate != 1 {
		t.Fatalf("恢复后状态应为正常: %+v", st)
	}
}

func TestWatermarkHysteresis(t *testing.T) {
	ctx := context.Background()
	svc, out := newTestServiceWith(t, Settings{
		SyslogMode: "tcp", SamplingMode: "all", SampleRate: 10,
		WebhookEnabled: true, WebhookURL: "https://siem/hook",
	})
	pct := 95.0
	wm := NewWatermark(svc, func() (float64, error) { return pct, nil },
		WatermarkConfig{High: 90, Low: 85, Consecutive: 2, Interval: time.Millisecond})
	if d, _ := wm.Evaluate(ctx); d {
		t.Fatal("首次命中不应立即降级")
	}
	if d, _ := wm.Evaluate(ctx); !d {
		t.Fatal("连续命中应降级")
	}
	got := readFile(t, out)
	if !sectionHas(got, "[transforms.ch_out]", "[transforms.log_out]", "rate = 10") {
		t.Fatalf("降级应保持内部 1/10 抽样:\n%s", got)
	}
	if !strings.Contains(got, `.severity != "INFO"`) {
		t.Fatalf("降级应渲染外发仅异常:\n%s", got)
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
	got = readFile(t, out)
	if strings.Contains(got, `.severity != "INFO"`) {
		t.Fatalf("恢复后不应保留外发仅异常:\n%s", got)
	}
	if !sectionHas(got, "[transforms.ch_out]", "[transforms.log_out]", "rate = 1") {
		t.Fatalf("恢复后内部入库应回到全量:\n%s", got)
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
		if k == "IPAM_LOG_DEGRADE_SAMPLE_RATE" {
			return "20"
		}
		return ""
	})
	if cfg.Interval != 3*time.Second || cfg.SampleRate != 20 {
		t.Fatalf("间隔/抽样比例解析失败: %+v", cfg)
	}
	cfg = WatermarkConfigFromEnv(func(k string) string {
		if k == "IPAM_LOG_DEGRADE_SAMPLE_RATE" {
			return "0"
		}
		return ""
	})
	if cfg.SampleRate != DefaultDegradeSampleRate {
		t.Fatalf("非法抽样比例应回退默认: %+v", cfg)
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
