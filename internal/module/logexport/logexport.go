// Package logexport 日志外发与采样策略（M6-008）：渲染 Vector 配置并热加载。
package logexport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	SyslogEnabled  bool
	SyslogAddress  string
	SyslogMode     string
	KafkaEnabled   bool
	KafkaBrokers   string
	KafkaTopic     string
	WebhookEnabled bool
	WebhookURL     string
	SamplingMode   string
	SampleRate     int
	UpdatedAt      time.Time
}

func Defaults() Settings {
	return Settings{SyslogMode: "tcp", SamplingMode: "all", SampleRate: 10}
}

type Repo interface {
	Get(ctx context.Context) (Settings, error)
	Save(ctx context.Context, s Settings) (Settings, error)
}

type Service struct {
	repo     Repo
	template string
	output   string
}

func NewService(repo Repo, template, output string) *Service {
	return &Service{repo: repo, template: template, output: output}
}

func (s *Service) Get(ctx context.Context) (Settings, error) { return s.repo.Get(ctx) }

func (s *Service) Apply(ctx context.Context) error {
	st, err := s.repo.Get(ctx)
	if err != nil {
		return err
	}
	return s.render(st)
}

func (s *Service) Save(ctx context.Context, st Settings) (Settings, error) {
	if err := Validate(st); err != nil {
		return Settings{}, err
	}
	if err := s.render(st); err != nil {
		return Settings{}, err
	}
	saved, err := s.repo.Save(ctx, st)
	if err != nil {
		return Settings{}, err
	}
	return saved, nil
}

func Validate(s Settings) error {
	if s.SyslogMode == "" {
		s.SyslogMode = "tcp"
	}
	if s.SyslogMode != "tcp" && s.SyslogMode != "udp" {
		return errors.New("syslogMode 仅支持 tcp/udp")
	}
	if s.SyslogEnabled && !strings.Contains(s.SyslogAddress, ":") {
		return errors.New("启用 Syslog 时必须填写 host:port")
	}
	if s.KafkaEnabled && (strings.TrimSpace(s.KafkaBrokers) == "" || strings.TrimSpace(s.KafkaTopic) == "") {
		return errors.New("启用 Kafka 时必须填写 brokers 与 topic")
	}
	if s.WebhookEnabled && !(strings.HasPrefix(s.WebhookURL, "http://") || strings.HasPrefix(s.WebhookURL, "https://")) {
		return errors.New("Webhook URL 必须是 http(s)")
	}
	if s.SamplingMode != "all" && s.SamplingMode != "sample" && s.SamplingMode != "errors" {
		return errors.New("samplingMode 仅支持 all/sample/errors")
	}
	if s.SampleRate < 1 || s.SampleRate > 1000 {
		return errors.New("sampleRate 需在 1-1000")
	}
	return nil
}

func (s *Service) render(st Settings) error {
	base, err := os.ReadFile(s.template)
	if err != nil {
		return fmt.Errorf("读取 Vector 模板失败: %w", err)
	}
	out := Render(string(base), st)
	dir := filepath.Dir(s.output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := s.output + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.output)
}

// Render 在基础模板上追加采样 transform 与外发 sink，并把 ClickHouse 输入改为 log_out。
func Render(base string, st Settings) string {
	// 仅替换 clickhouse sink 的输入（模板中唯一一行精确匹配）
	body := strings.Replace(base, "inputs = [\"normalize\"]", "inputs = [\"log_out\"]", 1)
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n# ── M6-008：采样 / 外发（由控制面渲染，勿手改）──\n")
	b.WriteString("[transforms.log_out]\n")
	b.WriteString("inputs = [\"normalize\"]\n")
	switch st.SamplingMode {
	case "errors":
		b.WriteString("type = \"filter\"\ncondition = '.severity != \"INFO\"'\n")
	default:
		rate := 1
		if st.SamplingMode == "sample" {
			rate = st.SampleRate
			if rate < 1 {
				rate = 1
			}
		}
		b.WriteString("type = \"sample\"\nrate = " + strconv.Itoa(rate) + "\n")
	}

	if st.SyslogEnabled {
		mode := st.SyslogMode
		if mode == "" {
			mode = "tcp"
		}
		b.WriteString("\n[sinks.siem_syslog]\n")
		b.WriteString("type = \"socket\"\n")
		b.WriteString("inputs = [\"log_out\"]\n")
		b.WriteString("address = " + strconv.Quote(strings.TrimSpace(st.SyslogAddress)) + "\n")
		b.WriteString("mode = " + strconv.Quote(mode) + "\n")
		b.WriteString("encoding.codec = \"syslog\"\n")
		b.WriteString("encoding.syslog.rfc = \"rfc5424\"\n")
		b.WriteString("encoding.syslog.app_name = \"ipam\"\n")
	}
	if st.KafkaEnabled {
		b.WriteString("\n[sinks.siem_kafka]\n")
		b.WriteString("type = \"kafka\"\n")
		b.WriteString("inputs = [\"log_out\"]\n")
		b.WriteString("bootstrap_servers = " + strconv.Quote(strings.TrimSpace(st.KafkaBrokers)) + "\n")
		b.WriteString("topic = " + strconv.Quote(strings.TrimSpace(st.KafkaTopic)) + "\n")
		b.WriteString("encoding.codec = \"json\"\n")
	}
	if st.WebhookEnabled {
		b.WriteString("\n[sinks.siem_webhook]\n")
		b.WriteString("type = \"http\"\n")
		b.WriteString("inputs = [\"log_out\"]\n")
		b.WriteString("uri = " + strconv.Quote(strings.TrimSpace(st.WebhookURL)) + "\n")
		b.WriteString("method = \"post\"\n")
		b.WriteString("encoding.codec = \"json\"\n")
		b.WriteString("framing.method = \"newline_delimited\"\n")
	}
	return b.String()
}
