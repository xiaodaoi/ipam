package dns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	guuid "github.com/google/uuid"

	"github.com/xiaodaoi/ipam/internal/pkg/rpz"
)

// 情报源（M6-039 P1 先行）：预设全部默认关闭，启用需确认许可（+ 必要时填密钥）。
//
// 设计约束：
//   - 拉取到的域名落磁盘缓存（<rpz dir>/sources/<key>.domains），百万级 IOC 不入库；
//   - 密钥落文件（secretDir，0600），库里只存路径，满足「密钥不入库」红线；
//   - 上游接口变更由用户改 URL/头部即可，不依赖发版。

var (
	ErrSourceNotFound     = errors.New("INTEL_SOURCE_NOT_FOUND")
	ErrSourceBuiltin      = errors.New("INTEL_SOURCE_BUILTIN")
	ErrSourceNoURL        = errors.New("INTEL_SOURCE_NO_URL")
	ErrSourceLicenseAck   = errors.New("INTEL_SOURCE_LICENSE_UNCONFIRMED")
	ErrSourceSecretNeeded = errors.New("INTEL_SOURCE_SECRET_REQUIRED")
	ErrSourceEmpty        = errors.New("INTEL_SOURCE_EMPTY")
	ErrSourceDupKey       = errors.New("INTEL_SOURCE_DUP")
	// ErrSecretDirUnwritable 密钥目录不可写（容器以非 root 运行时常见）。
	ErrSecretDirUnwritable = errors.New("INTEL_SECRET_DIR_UNWRITABLE")
)

// 单层容量上限（方案 §8：热层 ≤10 万、宽层 ≤30 万）。
const (
	MaxHotEntries   = 100_000
	MaxBroadEntries = 300_000
)

// IntelSource 情报源元数据。
type IntelSource struct {
	ID             string
	Key            string // 预设键或 custom:<uuid>
	Name           string
	Kind           string // preset | custom
	Layer          string // hot | broad
	URL            string
	Format         string
	AuthHeader     string // 例如 Auth-Key；为空表示匿名拉取
	SecretFile     string // 密钥文件路径（不回传内容）
	HasSecret      bool
	License        string
	LicenseURL     string
	CommercialOK   bool
	RequiresAck    bool
	AckedAt        *time.Time
	UpdateInterval int // 秒
	Enabled        bool
	Entries        int
	LastSyncAt     *time.Time
	LastStatus     string // never | ok | failed
	LastError      string
	Builtin        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PresetSources 内置源模板。全部默认关闭；URL 可被用户编辑以应对上游变更。
// 不内置 HaGeZi / OISD（GPL-3.0）与 Phishing Army（CC BY-NC）。
var PresetSources = []IntelSource{
	{
		Key: "abusech-urlhaus", Name: "abuse.ch URLhaus（恶意载荷分发站点）",
		Kind: "preset", Layer: "hot",
		// 2026-10 起 abuse.ch 关闭无鉴权下载：新增 v2 导出，Auth-Key 走 URL 路径。
		// URL 内保留 {secret} 占位符，拉取时才替换，密钥不入库、不回显。
		URL:    "https://urlhaus-api.abuse.ch/v2/files/exports/{secret}/hostfile.txt",
		Format: "auto", AuthHeader: "",
		License: "abuse.ch 社区条款（不授权用于商业产品）", LicenseURL: "https://auth.abuse.ch/",
		CommercialOK: false, RequiresAck: true, UpdateInterval: 300, Builtin: true,
	},
	{
		Key: "abusech-threatfox", Name: "abuse.ch ThreatFox（C2 / 载荷投递 IOC）",
		Kind: "preset", Layer: "hot",
		URL:    "https://threatfox-api.abuse.ch/v2/files/exports/{secret}/hostfile.txt",
		Format: "auto", AuthHeader: "",
		License: "abuse.ch 社区条款（不授权用于商业产品）", LicenseURL: "https://auth.abuse.ch/",
		CommercialOK: false, RequiresAck: true, UpdateInterval: 300, Builtin: true,
	},
	{
		Key: "spamhaus-dbl", Name: "Spamhaus DBL（低信誉域名，付费订阅）",
		Kind: "preset", Layer: "broad", URL: "",
		Format: "auto", AuthHeader: "Authorization",
		License: "Spamhaus 订阅条款（仅 phishing/malware，ZRD 不加载）", LicenseURL: "https://www.spamhaus.com/",
		CommercialOK: true, RequiresAck: true, UpdateInterval: 3600, Builtin: true,
	},
	{
		Key: "stevenblack-hosts", Name: "StevenBlack/hosts（综合聚合）",
		Kind: "preset", Layer: "broad", URL: "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		Format:  "auto",
		License: "MIT（聚合仓库，上游为混合许可）", LicenseURL: "https://github.com/StevenBlack/hosts",
		CommercialOK: true, RequiresAck: true, UpdateInterval: 86400, Builtin: true,
	},
	{
		Key: "openphish-community", Name: "OpenPhish Community（钓鱼 URL）",
		Kind: "preset", Layer: "hot", URL: "https://openphish.com/feed.txt",
		Format:  "auto",
		License: "Community 条款（商用需法务确认）", LicenseURL: "https://openphish.com/",
		CommercialOK: false, RequiresAck: true, UpdateInterval: 43200, Builtin: true,
	},
}

// SourceFetcher 拉取正文（header/secret 为空表示匿名）。
type SourceFetcher func(ctx context.Context, url, header, secret string) (string, error)

// sourceHTTPClient 情报源拉取专用（60s 超时；单次正文 ≤32MB）。
var sourceHTTPClient = &http.Client{Timeout: 60 * time.Second}

// HTTPSourceFetch 默认拉取器。
func HTTPSourceFetch(ctx context.Context, rawURL, header, secret string) (string, error) {
	// 部分上游（abuse.ch v2）要求密钥放在 URL 路径：URL 内用 {secret} 占位，
	// 仅在此处替换，库内与接口始终只有占位符。
	if secret != "" {
		rawURL = strings.ReplaceAll(rawURL, "{secret}", url.PathEscape(secret))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", scrubSecret(err, secret)
	}
	req.Header.Set("User-Agent", "ipam-intel/1.0")
	if header != "" && secret != "" {
		req.Header.Set(header, secret)
	}
	resp, err := sourceHTTPClient.Do(req)
	if err != nil {
		// net/http 的错误文本会带完整 URL；必须脱敏后再返回（lastError 会入库/回显）。
		return "", scrubSecret(err, secret)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", scrubSecret(err, secret)
	}
	return string(body), nil
}

// scrubSecret 从错误文本中抹掉密钥（含 URL 转义形态），防止密钥经 lastError 外泄。
func scrubSecret(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	msg := err.Error()
	msg = strings.ReplaceAll(msg, secret, "***")
	msg = strings.ReplaceAll(msg, url.PathEscape(secret), "***")
	return errors.New(msg)
}

// urlNeedsSecret URL 中是否使用了 {secret} 占位符（密钥走路径）。
func urlNeedsSecret(rawURL string) bool {
	return strings.Contains(rawURL, "{secret}")
}

// ParseFeedDomains 从 hosts / 域名清单 / URL 清单 / CSV / RPZ 文本中提取域名。
//
// 逐行取「第一个像域名的 token」：跳过 IP 字面量与数字、日期；
// `0.0.0.0 evil.com`、`evil.com`、`http://evil.com/x`、`"evil.com"`、
// `evil.com CNAME .`、`"1234","2026-01-01","http://evil.com/x"` 均可解析。
func ParseFeedDomains(body string) []string {
	seen := make(map[string]struct{}, 4096)
	out := make([]string, 0, 4096)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") ||
			strings.HasPrefix(line, "!") || strings.HasPrefix(line, "//") {
			continue
		}
		for _, tok := range feedTokens(line) {
			d := normalizeFeedDomain(tok)
			if d == "" {
				continue
			}
			if _, ok := seen[d]; ok {
				break
			}
			seen[d] = struct{}{}
			out = append(out, d)
			break
		}
	}
	return out
}

// feedTokens 把一行拆成候选 token（兼容空白、逗号、引号与括号）。
func feedTokens(line string) []string {
	fields := strings.FieldsFunc(line, func(r rune) bool {
		switch r {
		case ' ', '\t', ',', '"', '\'', '<', '>', '(', ')', '[', ']', '{', '}', '\r':
			return true
		}
		return false
	})
	return fields
}

// normalizeFeedDomain 把候选 token 规整成域名；不可识别返回空。
func normalizeFeedDomain(tok string) string {
	t := strings.TrimSpace(tok)
	if t == "" {
		return ""
	}
	if strings.Contains(t, "://") {
		if u, err := url.Parse(t); err == nil && u.Hostname() != "" {
			t = u.Hostname()
		}
	}
	// 去掉路径与端口
	if i := strings.IndexByte(t, '/'); i >= 0 {
		t = t[:i]
	}
	if host, _, err := net.SplitHostPort(t); err == nil {
		t = host
	}
	t = strings.ToLower(strings.TrimSuffix(t, "."))
	t = strings.TrimPrefix(t, "*.")
	if t == "" || t == "localhost" || t == "localhost.localdomain" ||
		t == "0.0.0.0" || t == "127.0.0.1" || t == "::1" {
		return ""
	}
	if net.ParseIP(t) != nil {
		return ""
	}
	if !strings.Contains(t, ".") || len(t) > 253 {
		return ""
	}
	for _, label := range strings.Split(t, ".") {
		if label == "" || len(label) > 63 {
			return ""
		}
		// 标签不得以连字符开头/结尾（RFC 1035）。
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ""
		}
		digitOnly := true
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			case c == '-', c == '_':
				digitOnly = false
			default:
				return ""
			}
			if c < '0' || c > '9' {
				digitOnly = false
			}
		}
		if digitOnly {
			return "" // 纯数字标签：多半是 ID/时间戳
		}
	}
	return t
}

// ── 服务方法 ──

// SetSourceFetcher 注入拉取器（单测用）。
func (s *IntelService) SetSourceFetcher(f SourceFetcher) { s.fetch = f }

// SetSecretDir 设置密钥文件目录（默认 /run/ipam/intel-secrets）。
func (s *IntelService) SetSecretDir(dir string) { s.secretDir = dir }

// SourceDomainPath 域名缓存的落盘路径（编译时按此读取）。
func (s *IntelService) SourceDomainPath(key string) string {
	return filepath.Join(s.dir, "sources", sanitizeKey(key)+".domains")
}

func sanitizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, key)
}

// EnsurePresets 首次落库预设（已存在则不覆盖：URL/开关/密钥由用户维护）。
func (s *IntelService) EnsurePresets(ctx context.Context) error {
	now := s.now().UTC()
	for _, p := range PresetSources {
		p.CreatedAt, p.UpdatedAt = now, now
		if err := s.store.UpsertPresetSource(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// Sources 返回全部源（预设 + 自定义）。
func (s *IntelService) Sources(ctx context.Context) ([]IntelSource, error) {
	list, err := s.store.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].HasSecret = list[i].SecretFile != "" && fileExists(list[i].SecretFile)
	}
	return list, nil
}

// CreateSource 新增自定义源（默认关闭）。
func (s *IntelService) CreateSource(ctx context.Context, src IntelSource) (IntelSource, error) {
	src.Name = strings.TrimSpace(src.Name)
	src.URL = strings.TrimSpace(src.URL)
	if src.Name == "" || src.URL == "" {
		return IntelSource{}, ErrSourceNoURL
	}
	if !strings.HasPrefix(src.URL, "https://") && !strings.HasPrefix(src.URL, "http://") {
		return IntelSource{}, fmt.Errorf("%w: URL 需以 http(s):// 开头", ErrSourceNoURL)
	}
	if src.Layer != "hot" && src.Layer != "broad" {
		src.Layer = "hot"
	}
	if src.UpdateInterval <= 0 {
		src.UpdateInterval = 21600
	}
	src.Kind = "custom"
	src.Format = "auto"
	src.Enabled = false
	// 自定义源使用 Go 侧 UUID，保证 key 与落盘缓存名稳定（不依赖 DB 返回值）。
	if src.ID == "" {
		src.ID = guuid.NewString()
	}
	if src.Key == "" {
		src.Key = "custom:" + src.ID
	}
	// 自定义源同样要求确认「自担许可责任」。
	src.RequiresAck = true
	if src.License == "" {
		src.License = "自定义源（许可由用户自担）"
	}
	return s.store.CreateSource(ctx, src)
}

// SourceUpdate 可编辑字段（全部可选）。
type SourceUpdate struct {
	Name           *string
	URL            *string
	Layer          *string
	AuthHeader     *string
	Secret         *string // 明文；写入文件，不入库
	ClearSecret    bool
	UpdateInterval *int
	Enabled        *bool
	AckLicense     bool
}

// UpdateSource 编辑/启停。启用前校验：URL、许可确认、必要密钥。
func (s *IntelService) UpdateSource(ctx context.Context, id string, up SourceUpdate) (IntelSource, error) {
	src, ok, err := s.store.GetSource(ctx, id)
	if err != nil {
		return IntelSource{}, err
	}
	if !ok {
		return IntelSource{}, ErrSourceNotFound
	}
	if up.Name != nil {
		src.Name = strings.TrimSpace(*up.Name)
	}
	if up.URL != nil {
		src.URL = strings.TrimSpace(*up.URL)
	}
	if up.Layer != nil && (*up.Layer == "hot" || *up.Layer == "broad") {
		src.Layer = *up.Layer
	}
	if up.AuthHeader != nil {
		src.AuthHeader = strings.TrimSpace(*up.AuthHeader)
	}
	if up.UpdateInterval != nil && *up.UpdateInterval > 0 {
		src.UpdateInterval = *up.UpdateInterval
	}
	if up.AckLicense {
		now := s.now().UTC()
		src.AckedAt = &now
	}
	if up.Secret != nil && strings.TrimSpace(*up.Secret) != "" {
		path, werr := s.writeSecret(src.ID, strings.TrimSpace(*up.Secret))
		if werr != nil {
			return IntelSource{}, werr
		}
		src.SecretFile = path
	}
	if up.ClearSecret {
		if src.SecretFile != "" {
			_ = os.Remove(src.SecretFile)
		}
		src.SecretFile = ""
	}
	src.UpdatedAt = s.now().UTC()

	enabling := up.Enabled != nil && *up.Enabled && !src.Enabled
	if up.Enabled != nil {
		src.Enabled = *up.Enabled
	}
	if src.Enabled {
		if err := validateSource(src); err != nil {
			src.Enabled = false
			return IntelSource{}, err
		}
	}
	if err := s.store.UpdateSource(ctx, src); err != nil {
		return IntelSource{}, err
	}
	s.invalidateAttribution()
	src.HasSecret = src.SecretFile != "" && fileExists(src.SecretFile)
	if enabling {
		// 启用即尝试同步一次：失败不回滚启用状态，但会记录 last_error 供 UI 展示。
		if synced, serr := s.SyncSource(ctx, src.ID); serr == nil {
			return synced, nil
		} else {
			log.Printf("[intel] 源 %s 启用后首次同步失败：%v", src.Key, serr)
			// 回读最新状态（failed + last_error），避免把「从未同步」的错误状态返回给前端。
			if latest, ok, lerr := s.store.GetSource(ctx, src.ID); lerr == nil && ok {
				latest.HasSecret = latest.SecretFile != "" && fileExists(latest.SecretFile)
				return latest, nil
			}
		}
	}
	return src, nil
}

// DeleteSource 删除自定义源（预设不可删除，只能停用）。
func (s *IntelService) DeleteSource(ctx context.Context, id string) error {
	src, ok, err := s.store.GetSource(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSourceNotFound
	}
	if src.Builtin {
		return ErrSourceBuiltin
	}
	if src.SecretFile != "" {
		_ = os.Remove(src.SecretFile)
	}
	_ = os.Remove(s.SourceDomainPath(src.Key))
	if err := s.store.DeleteSource(ctx, id); err != nil {
		return err
	}
	s.invalidateAttribution()
	_, _ = s.Compile(ctx)
	return nil
}

// SyncSource 拉取 → 解析 → 落缓存 → 重编译（失败保留上一版数据）。
func (s *IntelService) SyncSource(ctx context.Context, id string) (IntelSource, error) {
	src, ok, err := s.store.GetSource(ctx, id)
	if err != nil {
		return IntelSource{}, err
	}
	if !ok {
		return IntelSource{}, ErrSourceNotFound
	}
	if !src.Enabled {
		return src, fmt.Errorf("源未启用：%s", src.Name)
	}
	secret := ""
	if src.SecretFile != "" {
		if b, rerr := os.ReadFile(src.SecretFile); rerr == nil {
			secret = strings.TrimSpace(string(b))
		}
	}
	body, ferr := s.fetch(ctx, src.URL, src.AuthHeader, secret)
	now := s.now().UTC()
	if ferr != nil {
		src.LastSyncAt = &now
		src.LastStatus = "failed"
		src.LastError = ferr.Error()
		_ = s.store.UpdateSourceSync(ctx, src.ID, src.Entries, now, "failed", src.LastError)
		return src, ferr
	}
	domains := ParseFeedDomains(body)
	if len(domains) == 0 {
		src.LastSyncAt = &now
		src.LastStatus = "failed"
		src.LastError = "拉取成功但解析到 0 条（上游格式可能已变更）"
		_ = s.store.UpdateSourceSync(ctx, src.ID, src.Entries, now, "failed", src.LastError)
		return src, ErrSourceEmpty
	}
	if err := writeDomainCache(s.SourceDomainPath(src.Key), domains); err != nil {
		return src, err
	}
	src.Entries = len(domains)
	src.LastSyncAt = &now
	src.LastStatus = "ok"
	src.LastError = ""
	src.HasSecret = src.SecretFile != "" && fileExists(src.SecretFile)
	if err := s.store.UpdateSourceSync(ctx, src.ID, src.Entries, now, "ok", ""); err != nil {
		return src, err
	}
	s.invalidateAttribution()
	if _, cerr := s.Compile(ctx); cerr != nil {
		log.Printf("[intel] 源 %s 同步后编译失败：%v", src.Key, cerr)
	}
	return src, nil
}

// SyncDueSources 同步到期的已启用源（调度器调用）；返回实际同步成功的源数。
func (s *IntelService) SyncDueSources(ctx context.Context) int {
	list, err := s.store.ListSources(ctx)
	if err != nil {
		return 0
	}
	now := s.now()
	okCount := 0
	for _, src := range list {
		if !src.Enabled || src.URL == "" {
			continue
		}
		due := src.LastSyncAt == nil
		if !due {
			interval := time.Duration(src.UpdateInterval) * time.Second
			if interval <= 0 {
				interval = 6 * time.Hour
			}
			due = now.Sub(*src.LastSyncAt) >= interval
		}
		if !due {
			continue
		}
		if _, serr := s.SyncSource(ctx, src.ID); serr != nil {
			log.Printf("[intel] 源 %s 同步失败：%v", src.Key, serr)
			continue
		}
		okCount++
	}
	return okCount
}

// StartIntelScheduler 每分钟检查一次到期源（无源启用时为空转）。
func StartIntelScheduler(ctx context.Context, svc *IntelService, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				svc.SyncDueSources(ctx)
			}
		}
	}()
}

// ── 编译用：读取各层域名 ──

// feedRecordsForLayer 汇总某层所有启用源的域名（去重、剔除白名单、按容量截断）。
func (s *IntelService) feedRecordsForLayer(sources []IntelSource, layer string, wl map[string]bool) []rpz.Record {
	out := make([]rpz.Record, 0, 1024)
	seen := make(map[string]struct{}, 1024)
	limit := MaxHotEntries
	if layer == "broad" {
		limit = MaxBroadEntries
	}
	for _, src := range sources {
		if !src.Enabled || src.Layer != layer {
			continue
		}
		domains, err := readDomainCache(s.SourceDomainPath(src.Key))
		if err != nil {
			log.Printf("[intel] 读取源 %s 缓存失败：%v", src.Key, err)
			continue
		}
		for _, d := range domains {
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			if whitelisted(d, wl) {
				continue
			}
			if len(out) >= limit {
				log.Printf("[intel] %s 层达到容量上限 %d，源 %s 及后续条目被截断", layer, limit, src.Key)
				return out
			}
			out = append(out, rpz.Record{Name: d, Action: rpz.ActionNXDomain})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── 工具 ──

func validateSource(src IntelSource) error {
	if strings.TrimSpace(src.URL) == "" {
		return ErrSourceNoURL
	}
	if src.RequiresAck && src.AckedAt == nil {
		return ErrSourceLicenseAck
	}
	if (strings.TrimSpace(src.AuthHeader) != "" || urlNeedsSecret(src.URL)) &&
		(src.SecretFile == "" || !fileExists(src.SecretFile)) {
		return ErrSourceSecretNeeded
	}
	return nil
}

func (s *IntelService) writeSecret(id, secret string) (string, error) {
	dir := s.secretDir
	if dir == "" {
		dir = filepath.Join(s.dir, "secrets")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("%w: 无法创建 %s（%v）；请确认该目录对控制面运行用户可写，或用 IPAM_INTEL_SECRET_DIR 指定可写目录",
			ErrSecretDirUnwritable, dir, err)
	}
	path := filepath.Join(dir, "intel-"+sanitizeKey(id))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("%w: 无法写入 %s（%v）；请确认目录权限或改用 IPAM_INTEL_SECRET_DIR",
			ErrSecretDirUnwritable, dir, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func writeDomainCache(path string, domains []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.Grow(len(domains) * 24)
	for _, d := range domains {
		sb.WriteString(d)
		sb.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readDomainCache(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if d := strings.TrimSpace(line); d != "" {
			out = append(out, d)
		}
	}
	return out, nil
}
