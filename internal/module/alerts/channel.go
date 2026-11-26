package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	guuid "github.com/google/uuid"
)

// 通道类型。
const (
	ChannelWebhook  = "webhook"
	ChannelEmail    = "email"
	ChannelFeishu   = "feishu"
	ChannelDingTalk = "dingtalk"
	ChannelWeCom    = "wecom"
	ChannelSlack    = "slack"
	ChannelTeams    = "teams"
)

// ChannelConfig 非敏感配置（密钥单独落 0600 文件）。
type ChannelConfig struct {
	URL        string   `json:"url,omitempty"`
	SMTPHost   string   `json:"smtpHost,omitempty"`
	SMTPPort   int      `json:"smtpPort,omitempty"`
	SMTPUser   string   `json:"smtpUser,omitempty"`
	SMTPFrom   string   `json:"smtpFrom,omitempty"`
	Recipients []string `json:"recipients,omitempty"`
}

// Channel 通知通道。
type Channel struct {
	ID          string
	Name        string
	Type        string
	Config      ChannelConfig
	SecretFile  string
	MinSeverity string
	QuietHours  string
	Enabled     bool
	LastTestAt  *time.Time
	LastStatus  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Delivery 通知投递记录。
type Delivery struct {
	ID          string
	AlertID     string
	ChannelID   string
	ChannelName string
	Status      string
	Attempt     int
	Error       string
	SentAt      time.Time
	CreatedAt   time.Time
}

// DeliveryFilter 投递记录过滤。
type DeliveryFilter struct {
	ChannelID string
	Status    string
	Limit     int
}

// ChannelPatch 通道局部更新。
type ChannelPatch struct {
	Name        *string
	URL         *string
	SMTPHost    *string
	SMTPPort    *int
	SMTPUser    *string
	SMTPFrom    *string
	Recipients  *[]string
	MinSeverity *string
	QuietHours  *string
	Enabled     *bool
}

func validChannelType(t string) bool {
	switch t {
	case ChannelWebhook, ChannelEmail, ChannelFeishu, ChannelDingTalk, ChannelWeCom, ChannelSlack, ChannelTeams:
		return true
	}
	return false
}

func normalizeChannel(ch Channel) (Channel, error) {
	ch.Name = strings.TrimSpace(ch.Name)
	ch.Type = strings.TrimSpace(ch.Type)
	if ch.Name == "" {
		return ch, fmt.Errorf("%w: 通道名称不能为空", ErrBadRequest)
	}
	if !validChannelType(ch.Type) {
		return ch, fmt.Errorf("%w: 通道类型非法", ErrBadRequest)
	}
	if ch.MinSeverity == "" {
		ch.MinSeverity = SeverityWarning
	}
	switch ch.MinSeverity {
	case SeverityCritical, SeverityWarning, SeverityInfo:
	default:
		return ch, fmt.Errorf("%w: 最小级别非法", ErrBadRequest)
	}
	if ch.QuietHours != "" {
		if _, _, err := parseQuietHours(ch.QuietHours); err != nil {
			return ch, err
		}
	}
	if ch.Type == ChannelEmail {
		if ch.Config.SMTPHost == "" || ch.Config.SMTPFrom == "" || len(ch.Config.Recipients) == 0 {
			return ch, fmt.Errorf("%w: 邮件通道需填写 SMTP 主机、发件人与收件人", ErrBadRequest)
		}
		if ch.Config.SMTPPort <= 0 {
			ch.Config.SMTPPort = 587
		}
	} else {
		u := strings.TrimSpace(ch.Config.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return ch, fmt.Errorf("%w: Webhook/IM 地址需以 http(s):// 开头", ErrBadRequest)
		}
		ch.Config.URL = u
	}
	return ch, nil
}

// parseQuietHours 解析 22:00-08:00 形式的勿扰时段。
func parseQuietHours(v string) (startMin, endMin int, err error) {
	parts := strings.SplitN(strings.TrimSpace(v), "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%w: 勿扰时段格式应为 HH:MM-HH:MM", ErrBadRequest)
	}
	parse := func(s string) (int, error) {
		hm := strings.SplitN(strings.TrimSpace(s), ":", 2)
		if len(hm) != 2 {
			return 0, fmt.Errorf("%w: 勿扰时段格式应为 HH:MM-HH:MM", ErrBadRequest)
		}
		h, e1 := strconv.Atoi(hm[0])
		m, e2 := strconv.Atoi(hm[1])
		if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, fmt.Errorf("%w: 勿扰时段格式应为 HH:MM-HH:MM", ErrBadRequest)
		}
		return h*60 + m, nil
	}
	if startMin, err = parse(parts[0]); err != nil {
		return 0, 0, err
	}
	if endMin, err = parse(parts[1]); err != nil {
		return 0, 0, err
	}
	return startMin, endMin, nil
}

func inQuietHours(v string, now time.Time) bool {
	start, end, err := parseQuietHours(v)
	if err != nil || v == "" {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	if start <= end {
		return cur >= start && cur < end
	}
	return cur >= start || cur < end
}

func (s *Service) writeSecret(id, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", nil
	}
	dir := s.secretDir
	if dir == "" {
		dir = "/run/ipam/notify-secrets"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建通知密钥目录失败: %w", err)
	}
	path := filepath.Join(dir, id+".secret")
	// 唯一临时文件 + rename（M6-062）：直接 WriteFile 在磁盘满/并发保存时
	// 会留下半截密钥，后续 HMAC 头为空导致通知静默失败。
	f, err := os.CreateTemp(dir, id+".secret.*.tmp")
	if err != nil {
		return "", fmt.Errorf("创建通知密钥临时文件失败: %w", err)
	}
	tmp := f.Name()
	if _, err := f.WriteString(secret); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("写入通知密钥失败: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("写入通知密钥失败: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("设置通知密钥权限失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("落盘通知密钥失败: %w", err)
	}
	return path, nil
}

func (s *Service) removeSecret(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// Channels 返回全部通道。
func (s *Service) Channels(ctx context.Context) ([]Channel, error) {
	if s.admin == nil {
		return []Channel{}, nil
	}
	return s.admin.ListChannels(ctx)
}

// CreateChannel 新增通道；secret 只写文件不入库。
func (s *Service) CreateChannel(ctx context.Context, ch Channel, secret, actor string) (Channel, error) {
	var err error
	ch, err = normalizeChannel(ch)
	if err != nil {
		return Channel{}, err
	}
	if s.admin == nil {
		return Channel{}, fmt.Errorf("%w: 通道存储不可用", ErrBadRequest)
	}
	if ch.ID == "" {
		ch.ID = guuid.NewString()
	}
	if ch.SecretFile, err = s.writeSecret(ch.ID, secret); err != nil {
		return Channel{}, err
	}
	out, err := s.admin.CreateChannel(ctx, ch)
	if err != nil && ch.SecretFile != "" {
		s.removeSecret(ch.SecretFile)
	}
	return out, err
}

// UpdateChannel 局部更新通道。
func (s *Service) UpdateChannel(ctx context.Context, id string, patch ChannelPatch, secret string, clearSecret bool, actor string) (Channel, error) {
	if s.admin == nil {
		return Channel{}, fmt.Errorf("%w: 通道存储不可用", ErrBadRequest)
	}
	ch, ok, err := s.admin.GetChannel(ctx, id)
	if err != nil {
		return Channel{}, err
	}
	if !ok {
		return Channel{}, ErrNotFound
	}
	if patch.Name != nil {
		ch.Name = *patch.Name
	}
	if patch.URL != nil {
		ch.Config.URL = *patch.URL
	}
	if patch.SMTPHost != nil {
		ch.Config.SMTPHost = *patch.SMTPHost
	}
	if patch.SMTPPort != nil {
		ch.Config.SMTPPort = *patch.SMTPPort
	}
	if patch.SMTPUser != nil {
		ch.Config.SMTPUser = *patch.SMTPUser
	}
	if patch.SMTPFrom != nil {
		ch.Config.SMTPFrom = *patch.SMTPFrom
	}
	if patch.Recipients != nil {
		ch.Config.Recipients = *patch.Recipients
	}
	if patch.MinSeverity != nil {
		ch.MinSeverity = *patch.MinSeverity
	}
	if patch.QuietHours != nil {
		ch.QuietHours = *patch.QuietHours
	}
	if patch.Enabled != nil {
		ch.Enabled = *patch.Enabled
	}
	oldSecret := ch.SecretFile
	if clearSecret {
		ch.SecretFile = ""
	}
	if strings.TrimSpace(secret) != "" {
		// 先原子写入新密钥，DB 更新成功后再删旧路径（M6-062）：
		// 先删后写会在 DB 更新失败/磁盘异常时让通道永久丢失密钥。
		if ch.SecretFile, err = s.writeSecret(ch.ID, secret); err != nil {
			return Channel{}, err
		}
	}
	ch, err = normalizeChannel(ch)
	if err != nil {
		return Channel{}, err
	}
	out, err := s.admin.UpdateChannel(ctx, ch)
	if err != nil {
		return out, err
	}
	if oldSecret != "" && oldSecret != out.SecretFile {
		s.removeSecret(oldSecret)
	}
	return out, nil
}

// DeleteChannel 删除通道。
func (s *Service) DeleteChannel(ctx context.Context, id string) error {
	if s.admin == nil {
		return fmt.Errorf("%w: 通道存储不可用", ErrBadRequest)
	}
	ch, ok, err := s.admin.GetChannel(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	// 先删 DB 行，成功后再删密钥文件（M6-062）：
	// 反序在 DB 删除失败时会让仍存在的通道失去密钥。
	if err := s.admin.DeleteChannel(ctx, id); err != nil {
		return err
	}
	s.removeSecret(ch.SecretFile)
	return nil
}

// Deliveries 查询投递记录。
func (s *Service) Deliveries(ctx context.Context, f DeliveryFilter) ([]Delivery, int, error) {
	if s.admin == nil {
		return []Delivery{}, 0, nil
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	return s.admin.ListDeliveries(ctx, f)
}

// TestChannel 发送测试通知。
func (s *Service) TestChannel(ctx context.Context, id string) (bool, string, error) {
	if s.admin == nil {
		return false, "通道存储不可用", ErrBadRequest
	}
	ch, ok, err := s.admin.GetChannel(ctx, id)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", ErrNotFound
	}
	d := NewDispatcher(s.admin, s.secretDir, "", log.Printf)
	sample := Alert{
		ID: "00000000-0000-0000-0000-000000000000", Source: SourceSystem, Kind: "test",
		Severity: SeverityWarning, Title: "IPAM 测试通知", Message: "这是一条测试通知，收到即表示通道配置正确。",
		Count: 1, LastSeen: s.now(),
	}
	if err := d.SendTo(ctx, ch, sample); err != nil {
		status := "failed: " + err.Error()
		ch.LastStatus = status
		now := s.now()
		ch.LastTestAt = &now
		_, _ = s.admin.UpdateChannel(ctx, ch)
		return false, status, nil
	}
	ch.LastStatus = "ok"
	now := s.now()
	ch.LastTestAt = &now
	_, _ = s.admin.UpdateChannel(ctx, ch)
	return true, "ok", nil
}

// ── 通知分发 ──

// Notifier 通知分发接口（采集器只依赖该接口，便于单测替换）。
type Notifier interface {
	Notify(ctx context.Context, a Alert)
}

// Dispatcher 通知分发器：按通道级别/勿扰时段发送，并记录投递结果。
//
// M6-056：投递必须与采集协程解耦。此前 Notify 在采集循环内同步发送，
// 一条 SMTP 卡死就会让 DNS 采集/系统探针/自动恢复/每日清理整体停摆。
type Dispatcher struct {
	admin     AdminRepo
	secretDir string
	publicURL string
	logf      func(format string, args ...any)
	client    *http.Client

	jobs        chan Alert
	startOnce   sync.Once
	sendTimeout time.Duration
	retries     int
}

const (
	notifyQueueSize    = 1024
	notifyWorkers      = 2
	defaultSendTimeout = 10 * time.Second
	defaultRetries     = 2
)

// NewDispatcher 构造。
func NewDispatcher(admin AdminRepo, secretDir, publicURL string, logf func(string, ...any)) *Dispatcher {
	if logf == nil {
		logf = log.Printf
	}
	return &Dispatcher{admin: admin, secretDir: secretDir, publicURL: strings.TrimRight(publicURL, "/"),
		logf: logf, client: &http.Client{Timeout: 6 * time.Second},
		jobs: make(chan Alert, notifyQueueSize), sendTimeout: defaultSendTimeout, retries: defaultRetries}
}

// Notify 入队后立即返回；由后台 worker 带超时/重试发送。
func (d *Dispatcher) Notify(_ context.Context, a Alert) {
	if d == nil || d.admin == nil || !a.Notify {
		return
	}
	d.startOnce.Do(func() {
		for i := 0; i < notifyWorkers; i++ {
			go d.worker()
		}
	})
	select {
	case d.jobs <- a:
	default:
		// 队列满说明下游持续失败：不能阻塞采集循环，记录后丢弃（投递记录会缺行）。
		d.logf("[alerts] 通知队列已满（%d），丢弃报警 %s", notifyQueueSize, a.ID)
	}
}

func (d *Dispatcher) worker() {
	for a := range d.jobs {
		d.deliver(a)
	}
}

// deliver 在后台上下文里发送一条报警的所有匹配通道。
func (d *Dispatcher) deliver(a Alert) {
	listCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	channels, err := d.admin.ListChannels(listCtx)
	cancel()
	if err != nil {
		d.logf("[alerts] 读取通知通道失败: %v", err)
		return
	}
	now := time.Now()
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if severityRank(a.Severity) < severityRank(ch.MinSeverity) {
			continue
		}
		if a.Severity != SeverityCritical && inQuietHours(ch.QuietHours, now) {
			continue
		}
		attempts := 0
		var lastErr error
		for attempts <= d.retries {
			attempts++
			sctx, scancel := context.WithTimeout(context.Background(), d.sendTimeout)
			lastErr = d.SendTo(sctx, ch, a)
			scancel()
			if lastErr == nil {
				break
			}
			if attempts <= d.retries {
				// 指数退避：1s、5s（瞬时 5xx/网络抖动可自愈）
				time.Sleep(time.Duration(1+4*(attempts-1)) * time.Second)
			}
		}
		status, errText := "sent", ""
		if lastErr != nil {
			status, errText = "failed", lastErr.Error()
			d.logf("[alerts] 通道 %s 投递失败（%d 次）: %v", ch.Name, attempts, lastErr)
		}
		recCtx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.admin.CreateDelivery(recCtx, Delivery{
			ID: guuid.NewString(), AlertID: a.ID, ChannelID: ch.ID, ChannelName: ch.Name,
			Status: status, Attempt: attempts, Error: errText, SentAt: time.Now(),
		})
		rcancel()
	}
}

// SendTo 向单个通道发送一条报警。
func (d *Dispatcher) SendTo(ctx context.Context, ch Channel, a Alert) error {
	secret := readSecretFile(ch.SecretFile)
	switch ch.Type {
	case ChannelEmail:
		return sendEmail(ctx, ch, secret, a)
	default:
		return d.sendWebhook(ctx, ch, secret, a)
	}
}

func readSecretFile(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func (d *Dispatcher) sendWebhook(ctx context.Context, ch Channel, secret string, a Alert) error {
	body := buildPayload(ch.Type, d.link(a), a)
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.Config.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "IPAM-Alerting/1.0")
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		req.Header.Set("X-IPAM-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) link(a Alert) string {
	if a.ID == "" {
		return d.publicURL
	}
	return d.publicURL + "/logs-center/alerts?alertId=" + a.ID
}

func buildPayload(kind, link string, a Alert) map[string]any {
	text := fmt.Sprintf("[%s] %s：%s", strings.ToUpper(a.Severity), a.Title, a.Message)
	if a.Count > 1 {
		text += fmt.Sprintf("（%d 次）", a.Count)
	}
	if link != "" {
		text += "\n" + link
	}
	switch kind {
	case ChannelFeishu:
		return map[string]any{"msg_type": "text", "content": map[string]any{"text": text}}
	case ChannelDingTalk:
		return map[string]any{"msgtype": "text", "text": map[string]any{"content": text}}
	case ChannelWeCom:
		return map[string]any{"msgtype": "text", "text": map[string]any{"content": text}}
	case ChannelSlack, ChannelTeams:
		return map[string]any{"text": text}
	default:
		return map[string]any{
			"title": a.Title, "message": a.Message, "text": text,
			"severity": a.Severity, "source": a.Source, "kind": a.Kind,
			"domain": a.Domain, "clientIp": a.ClientIP, "count": a.Count,
			"firstSeen": a.FirstSeen, "lastSeen": a.LastSeen, "link": link,
		}
	}
}

func sendEmail(ctx context.Context, ch Channel, secret string, a Alert) error {
	host := ch.Config.SMTPHost
	port := ch.Config.SMTPPort
	if port <= 0 {
		port = 587
	}
	addr := host + ":" + strconv.Itoa(port)
	var auth smtp.Auth
	if ch.Config.SMTPUser != "" && secret != "" {
		auth = smtp.PlainAuth("", ch.Config.SMTPUser, secret, host)
	}
	subject := fmt.Sprintf("[IPAM][%s] %s", strings.ToUpper(a.Severity), a.Title)
	body := a.Message
	if a.Count > 1 {
		body += fmt.Sprintf("\r\n\r\n窗口内次数：%d", a.Count)
	}
	msg := []byte("From: " + ch.Config.SMTPFrom + "\r\n" +
		"To: " + strings.Join(ch.Config.Recipients, ",") + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	// 标准库 smtp.SendMail 无超时：SMTP 主机可建连但不回包时会永久阻塞。
	// 这里自己建连并设置 deadline，保证每次投递有界（M6-056）。
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(ch.Config.SMTPFrom); err != nil {
		return err
	}
	for _, rcpt := range ch.Config.Recipients {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
