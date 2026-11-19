package dns

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M6-039：情报源解析、启用校验、同步回退、分层编译与容量截断。

func TestParseFeedDomains_兼容多格式(t *testing.T) {
	body := strings.Join([]string{
		"# comment line",
		"; another comment",
		"! rpz comment",
		"0.0.0.0 ads.example.com",
		"127.0.0.1 tracker.example.net",
		"malware.example.org",
		"http://phish.example.com/login?x=1",
		"\"1234\",\"2026-01-01 00:00:00\",\"http://bad.example.io/path\"",
		"evil.example.cn CNAME .",
		"*.wild.example.com",
		"10.0.0.1",
		"1.2.3.4",
		"localhost",
		"",
	}, "\n")

	got := ParseFeedDomains(body)
	want := []string{
		"ads.example.com", "tracker.example.net", "malware.example.org",
		"phish.example.com", "bad.example.io", "evil.example.cn", "wild.example.com",
	}
	if len(got) != len(want) {
		t.Fatalf("解析结果数量不符：got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条不符：got=%q want=%q（全部=%v）", i, got[i], want[i], got)
		}
	}
}

func TestParseFeedDomains_去重且跳过纯数字与非法(t *testing.T) {
	body := strings.Join([]string{
		"a.example.com",
		"a.example.com",
		"0.0.0.0 a.example.com",
		"123456",
		"2026-11-19",
		"-bad-.example.com",
		"too..many.dots.example.com",
		"ok.example.com",
	}, "\n")
	got := ParseFeedDomains(body)
	if strings.Join(got, ",") != "a.example.com,ok.example.com" {
		t.Fatalf("去重/过滤结果不符: %v", got)
	}
}

func TestNormalizeFeedDomain_边界(t *testing.T) {
	cases := map[string]string{
		"https://Example.COM.:443/x": "example.com",
		"*.Sub.Example.com":          "sub.example.com",
		"host.example.com:8080":      "host.example.com",
		"http://evil.example.com":    "evil.example.com",
		"10.1.1.1":                   "",
		"::1":                        "",
		"nodot":                      "",
	}
	for in, want := range cases {
		if got := normalizeFeedDomain(in); got != want {
			t.Errorf("normalizeFeedDomain(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestIntelSource_预设默认全部关闭且不可删除(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	if err := st.intel.EnsurePresets(ctx); err != nil {
		t.Fatal(err)
	}
	// 幂等：再次落库不新增。
	if err := st.intel.EnsurePresets(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := st.intel.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(PresetSources) {
		t.Fatalf("预设数量不符：got=%d want=%d", len(list), len(PresetSources))
	}
	for _, s := range list {
		if s.Enabled {
			t.Errorf("预设 %s 应默认关闭", s.Key)
		}
		if !s.RequiresAck {
			t.Errorf("预设 %s 应要求许可确认", s.Key)
		}
		if !s.Builtin {
			t.Errorf("预设 %s 应标记 builtin", s.Key)
		}
	}
	if err := st.intel.DeleteSource(ctx, list[0].ID); !errors.Is(err, ErrSourceBuiltin) {
		t.Fatalf("预设应不可删除，got=%v", err)
	}
}

func TestIntelSource_启用校验许可与密钥(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()
	if err := st.intel.EnsurePresets(ctx); err != nil {
		t.Fatal(err)
	}

	// 1) 自定义源未确认许可 → 拒绝启用。
	enabled := true
	custom, err := st.intel.CreateSource(ctx, IntelSource{
		Name: "内部黑名单", URL: "https://intranet.example/domains.txt", Layer: "hot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.Enabled {
		t.Fatal("新建源应默认关闭")
	}
	if _, err := st.intel.UpdateSource(ctx, custom.ID, SourceUpdate{Enabled: &enabled}); !errors.Is(err, ErrSourceLicenseAck) {
		t.Fatalf("未确认许可应报 ErrSourceLicenseAck，got=%v", err)
	}

	// 2) 需要鉴权的预设：确认许可但缺密钥 → 拒绝。
	var urlhaus IntelSource
	for _, s := range mustSources(t, st.intel, ctx) {
		if s.Key == "abusech-urlhaus" {
			urlhaus = s
		}
	}
	if urlhaus.ID == "" {
		t.Fatal("缺少 urlhaus 预设")
	}
	if _, err := st.intel.UpdateSource(ctx, urlhaus.ID, SourceUpdate{Enabled: &enabled, AckLicense: true}); !errors.Is(err, ErrSourceSecretNeeded) {
		t.Fatalf("缺密钥应报 ErrSourceSecretNeeded，got=%v", err)
	}

	// 3) 写入密钥后允许启用（首次同步失败不阻止启用）。
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "0.0.0.0 evil.example.com\n", nil
	})
	secret := "test-key"
	got, err := st.intel.UpdateSource(ctx, urlhaus.ID, SourceUpdate{Enabled: &enabled, AckLicense: true, Secret: &secret})
	if err != nil {
		t.Fatalf("写密钥后启用失败: %v", err)
	}
	if !got.Enabled || !got.HasSecret {
		t.Fatalf("启用状态不符: enabled=%v hasSecret=%v", got.Enabled, got.HasSecret)
	}
	if got.Entries != 1 || got.LastStatus != "ok" {
		t.Fatalf("启用后首次同步未生效: entries=%d status=%s", got.Entries, got.LastStatus)
	}
}

func TestIntelSource_同步失败保留上一版(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()

	src, err := st.intel.CreateSource(ctx, IntelSource{
		Name: "自定义", URL: "https://feed.example/list.txt", Layer: "hot",
	})
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "bad-one.example.com\nbad-two.example.com\n", nil
	})
	if _, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Enabled: &enabled, AckLicense: true}); err != nil {
		t.Fatal(err)
	}
	cache := st.intel.SourceDomainPath(src.Key)
	before, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("缓存未落盘: %v", err)
	}

	// 上游失败：状态 failed，但旧数据保留（fail-safe 继续拦截）。
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "", errors.New("upstream 503")
	})
	after, err := st.intel.SyncSource(ctx, src.ID)
	if err == nil {
		t.Fatal("上游失败应返回错误")
	}
	if after.LastStatus != "failed" || after.LastError == "" {
		t.Fatalf("失败状态未记录: status=%s err=%s", after.LastStatus, after.LastError)
	}
	now, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("旧缓存被删除: %v", err)
	}
	if string(now) != string(before) {
		t.Fatalf("失败同步不应覆盖缓存:\nbefore=%s\nafter=%s", before, now)
	}

	// 空结果同样视为失败，保留旧数据。
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "<html>changed api</html>", nil
	})
	if _, err := st.intel.SyncSource(ctx, src.ID); !errors.Is(err, ErrSourceEmpty) {
		t.Fatalf("空结果应报 ErrSourceEmpty，got=%v", err)
	}
	if now, _ := os.ReadFile(cache); string(now) != string(before) {
		t.Fatal("空结果不应覆盖缓存")
	}
}

func TestIntelSource_分层编译剔除白名单(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()

	hot, err := st.intel.CreateSource(ctx, IntelSource{Name: "热层", URL: "https://a.example/hot.txt", Layer: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	broad, err := st.intel.CreateSource(ctx, IntelSource{Name: "宽层", URL: "https://a.example/broad.txt", Layer: "broad"})
	if err != nil {
		t.Fatal(err)
	}
	// 先启用（离线夹具，避免真实网络），再写缓存绕开拉取。
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "placeholder.example.com\n", nil
	})
	for _, id := range []string{hot.ID, broad.ID} {
		if _, err := st.intel.UpdateSource(ctx, id, SourceUpdate{Enabled: boolPtr(true), AckLicense: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(hot.Key), []string{
		"evil.example.com", "ads.example.net", "keep.example.org",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(broad.Key), []string{
		"broad.example.io",
	}); err != nil {
		t.Fatal(err)
	}
	// 白名单：suffix example.net 应剔除 ads.example.net（含子域语义）。
	if _, err := st.store.CreateWhitelist(ctx, IntelWhitelist{
		Kind: "suffix", Value: "example.net", Reason: "误报", Source: "manual", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.intel.Compile(ctx); err != nil {
		t.Fatal(err)
	}
	hotZone := st.zone(t, ZoneHot)
	if !strings.Contains(hotZone, "evil.example.com") || !strings.Contains(hotZone, "keep.example.org") {
		t.Fatalf("热层应含源条目:\n%s", hotZone)
	}
	if strings.Contains(hotZone, "ads.example.net") {
		t.Fatalf("白名单域名不应进入热层:\n%s", hotZone)
	}
	broadZone := st.zone(t, ZoneBroad)
	if !strings.Contains(broadZone, "broad.example.io") {
		t.Fatalf("宽层应含源条目:\n%s", broadZone)
	}
	if strings.Contains(hotZone, "broad.example.io") {
		t.Fatalf("宽层条目不应进入热层:\n%s", hotZone)
	}

	// 停用后条目应消失。
	if _, err := st.intel.UpdateSource(ctx, hot.ID, SourceUpdate{Enabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.intel.Compile(ctx); err != nil {
		t.Fatal(err)
	}
	if z := st.zone(t, ZoneHot); strings.Contains(z, "evil.example.com") {
		t.Fatalf("停用源条目应被移除:\n%s", z)
	}
}

func TestIntelSource_超容量截断(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()

	// 略超上限，验证按层容量截断（方案 §8：热层 ≤10 万）。
	limit := MaxHotEntries + 5
	src, err := st.intel.CreateSource(ctx, IntelSource{Name: "超量", URL: "https://a.example/big.txt", Layer: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	domains := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		domains = append(domains, fmt.Sprintf("d%d.example.com", i))
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(src.Key), domains); err != nil {
		t.Fatal(err)
	}
	src.Enabled = true
	recs := st.intel.feedRecordsForLayer([]IntelSource{src}, "hot", map[string]bool{})
	if len(recs) != MaxHotEntries {
		t.Fatalf("热层应截断到 %d，实际 %d", MaxHotEntries, len(recs))
	}
}

func TestIntelSource_自定义源删除清理缓存(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()
	src, err := st.intel.CreateSource(ctx, IntelSource{Name: "待删", URL: "https://a.example/x.txt", Layer: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	cache := st.intel.SourceDomainPath(src.Key)
	if err := writeDomainCache(cache, []string{"x.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := st.intel.DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("删除源应清理缓存，stat err=%v", err)
	}
	if _, ok, _ := st.store.GetSource(ctx, src.ID); ok {
		t.Fatal("源应已从存储删除")
	}
}

func TestIntelSource_密钥文件不入库(t *testing.T) {
	st := newIntelStack(t)
	secretDir := t.TempDir()
	st.intel.SetSecretDir(secretDir)
	ctx := context.Background()

	src, err := st.intel.CreateSource(ctx, IntelSource{Name: "鉴权源", URL: "https://a.example/x.txt", Layer: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	secret := "very-secret-token"
	got, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Secret: &secret})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasSecret || got.SecretFile == "" {
		t.Fatal("应记录密钥文件")
	}
	if !strings.HasPrefix(got.SecretFile, secretDir) {
		t.Fatalf("密钥文件应落在 secretDir: %s", got.SecretFile)
	}
	info, err := os.Stat(got.SecretFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("密钥文件权限应为 0600，实际 %o", info.Mode().Perm())
	}
	// 清除密钥。
	if _, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{ClearSecret: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(secretDir, "intel-"+sanitizeKey(src.ID))); !os.IsNotExist(err) {
		t.Fatalf("clearSecret 应删除密钥文件: %v", err)
	}
}

func mustSources(t *testing.T, svc *IntelService, ctx context.Context) []IntelSource {
	t.Helper()
	list, err := svc.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func boolPtr(b bool) *bool { return &b }
