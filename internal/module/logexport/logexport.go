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
	"sync"
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

// Status 运行期生效状态（M6-008/M6-043 自动降级）：用户策略仍保存在 repo，
// 磁盘水位触发时仅覆盖本次渲染，不修改用户配置。
type Status struct {
	// EffectiveSamplingMode 外发通道实际生效模式（all/sample/errors）。
	EffectiveSamplingMode string
	// EffectiveIngestSampleRate 内部入库实际采样率 1/N（1=全量；降级时 N>1）。
	EffectiveIngestSampleRate int
	Degraded                  bool
	DiskUsedPercent           float64
	DegradedAt                time.Time
}

// DefaultDegradeSampleRate 自动降级时内部入库的抽样比例（1/N）。
// 降级只降低写入量，绝不清零 INFO，避免仪表盘/日志概览出现误导性空白（M6-043）。
const DefaultDegradeSampleRate = 10

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

	mu              sync.Mutex
	autoMode        string // 非空=磁盘水位自动降级中
	effectiveMode   string // 最近一次渲染外发实际模式
	effectiveIngest int    // 最近一次渲染内部入库采样率 1/N
	degradeSample   int    // 自动降级时内部入库采样率 1/N
	diskPct         float64
	degradedAt      time.Time
}

func NewService(repo Repo, template, output string) *Service {
	return &Service{repo: repo, template: template, output: output, degradeSample: DefaultDegradeSampleRate}
}

// SetDegradeSampleRate 覆盖自动降级时的内部入库采样率（1/N，默认 10）。
func (s *Service) SetDegradeSampleRate(n int) {
	if n < 1 {
		n = 1
	}
	if n > 1000 {
		n = 1000
	}
	s.mu.Lock()
	s.degradeSample = n
	s.mu.Unlock()
}

func (s *Service) degradeRate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.degradeSample < 1 {
		return DefaultDegradeSampleRate
	}
	return s.degradeSample
}

func (s *Service) Get(ctx context.Context) (Settings, error) { return s.repo.Get(ctx) }

func (s *Service) Apply(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.repo.Get(ctx)
	if err != nil {
		return err
	}
	return s.renderLocked(st, s.autoMode)
}

func (s *Service) Save(ctx context.Context, st Settings) (Settings, error) {
	if err := Validate(st); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, err := s.repo.Save(ctx, st)
	if err != nil {
		return Settings{}, err
	}
	// 自动降级期间保持覆盖，避免用户保存后短暂恢复全量写入。
	if err := s.renderLocked(saved, s.autoMode); err != nil {
		return Settings{}, err
	}
	return saved, nil
}

// SetAutoSampling 应用/解除磁盘水位自动降级（M6-008）。
// mode 为空表示恢复正常（回到用户配置）；失败时不改变当前覆盖状态。
func (s *Service) SetAutoSampling(ctx context.Context, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.autoMode == mode {
		return nil
	}
	st, err := s.repo.Get(ctx)
	if err != nil {
		return err
	}
	if err := s.renderLocked(st, mode); err != nil {
		return err
	}
	s.autoMode = mode
	if mode == "" {
		s.degradedAt = time.Time{}
	} else {
		s.degradedAt = time.Now().UTC()
	}
	return nil
}

// SetDiskPercent 记录最近一次磁盘水位（供 API/页面展示）。
func (s *Service) SetDiskPercent(pct float64) {
	s.mu.Lock()
	s.diskPct = pct
	s.mu.Unlock()
}

// Status 返回当前生效状态。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	mode := s.effectiveMode
	ingest := s.effectiveIngest
	if ingest < 1 {
		ingest = 1
	}
	return Status{
		EffectiveSamplingMode:     mode,
		EffectiveIngestSampleRate: ingest,
		Degraded:                  s.autoMode != "",
		DiskUsedPercent:           s.diskPct,
		DegradedAt:                s.degradedAt,
	}
}

// AutoMode 当前自动降级覆盖模式（空=未降级）。
func (s *Service) AutoMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoMode
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
	if s.WebhookEnabled && !strings.HasPrefix(s.WebhookURL, "http://") && !strings.HasPrefix(s.WebhookURL, "https://") {
		return errors.New("webhook URL 必须是 http(s)")
	}
	if s.SamplingMode != "all" && s.SamplingMode != "sample" && s.SamplingMode != "errors" {
		return errors.New("samplingMode 仅支持 all/sample/errors")
	}
	if s.SampleRate < 1 || s.SampleRate > 1000 {
		return errors.New("sampleRate 需在 1-1000")
	}
	return nil
}

func (s *Service) renderLocked(st Settings, autoMode string) error {
	ingestRate := 1
	if autoMode != "" {
		// 自动降级：内部入库按 1/N 抽样（保留 INFO），外发收窄为仅异常。
		st.SamplingMode = "errors"
		ingestRate = s.degradeSample
		if ingestRate < 1 {
			ingestRate = DefaultDegradeSampleRate
		}
	}
	base, err := os.ReadFile(s.template)
	if err != nil {
		return fmt.Errorf("读取 Vector 模板失败: %w", err)
	}
	out := Render(string(base), st, ingestRate)
	dir := filepath.Dir(s.output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := s.output + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.output); err != nil {
		return err
	}
	s.effectiveMode = st.SamplingMode
	s.effectiveIngest = ingestRate
	return nil
}

// Render 在基础模板上生成两条独立链路（M6-043）：
//
//	normalize → ch_out(内部入库，1/N 采样) → clickhouse
//	normalize → log_out(外发策略) → siem_syslog/kafka/webhook
//
// 关键点：ClickHouse 永远接 ch_out，绝不被外发 errors 过滤器清零；
// ingestRate=1 表示全量入库，>1 表示磁盘水位降级期间的 1/N 抽样。
func Render(base string, st Settings, ingestRate int) string {
	if ingestRate < 1 {
		ingestRate = 1
	}
	// 仅替换 clickhouse sink 的输入（模板中唯一精确匹配）
	body := strings.Replace(base, "inputs = [\"normalize\"]", "inputs = [\"ch_out\"]", 1)
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n# ── M6-043：内部入库链路（全量 / 降级抽样，永不丢弃 INFO）──\n")
	b.WriteString("[transforms.ch_out]\n")
	b.WriteString("inputs = [\"normalize\"]\n")
	b.WriteString("type = \"sample\"\n")
	b.WriteString("rate = " + strconv.Itoa(ingestRate) + "\n")

	// 外发链路仅在存在通道时渲染，避免无意义的事件副本。
	if st.SyslogEnabled || st.KafkaEnabled || st.WebhookEnabled {
		b.WriteString("\n# ── M6-008：外发采样（仅作用于 SIEM 通道）──\n")
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
