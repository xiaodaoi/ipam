package logexport

import (
	"strings"
	"testing"
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
