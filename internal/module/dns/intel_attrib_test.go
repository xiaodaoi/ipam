package dns

import (
	"context"
	"strings"
	"testing"
)

// M6-040 增强：报警来源归属（精确优先、后缀匹配、多源合并、变更失效）。

func TestAttributeDomain_后缀匹配与多源合并(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()

	hot, err := st.intel.CreateSource(ctx, IntelSource{
		Name: "热层源", URL: "https://a.example/hot.txt", Layer: "hot",
	})
	if err != nil {
		t.Fatal(err)
	}
	broad, err := st.intel.CreateSource(ctx, IntelSource{
		Name: "宽层源", URL: "https://a.example/broad.txt", Layer: "broad",
	})
	if err != nil {
		t.Fatal(err)
	}
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "placeholder.example.com\n", nil
	})
	enabled := true
	for _, id := range []string{hot.ID, broad.ID} {
		if _, err := st.intel.UpdateSource(ctx, id, SourceUpdate{Enabled: &enabled, AckLicense: true}); err != nil {
			t.Fatal(err)
		}
	}
	// 手动写入夹具（绕开拉取，覆盖精确/后缀/多源/共享域名）。
	if err := writeDomainCache(st.intel.SourceDomainPath(hot.Key), []string{
		"asdlot.org", "shared.example",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(broad.Key), []string{
		"evil.example", "shared.example",
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		domain    string
		wantKeys  string
		wantNames string
	}{
		// 精确命中热层源。
		{"asdlot.org", hot.Key, "热层源"},
		// 子域后缀命中（RPZ 对子域生效）。
		{"J8.ASDLOT.ORG.", hot.Key, "热层源"},
		{"deep.sub.evil.example", broad.Key, "宽层源"},
		// 两源共同收录：全部返回，便于前端展示多个来源（顺序按 key 稳定）。
		{"cdn.shared.example", "", ""},
		// 未命中（自定义封禁/内置规则）返回空。
		{"unknown.example", "", ""},
	}
	for _, c := range cases {
		keys, names := st.intel.AttributeDomain(ctx, c.domain)
		if c.domain == "cdn.shared.example" {
			if !strings.Contains(keys, hot.Key) || !strings.Contains(keys, broad.Key) ||
				!strings.Contains(names, "热层源") || !strings.Contains(names, "宽层源") {
				t.Errorf("多源归属应全部返回，got (%q,%q)", keys, names)
			}
			continue
		}
		if keys != c.wantKeys || names != c.wantNames {
			t.Errorf("AttributeDomain(%q)=(%q,%q)，期望 (%q,%q)",
				c.domain, keys, names, c.wantKeys, c.wantNames)
		}
	}
}

func TestAttributeDomain_源变更后索引失效(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()

	src, err := st.intel.CreateSource(ctx, IntelSource{
		Name: "待调整源", URL: "https://a.example/x.txt", Layer: "hot",
	})
	if err != nil {
		t.Fatal(err)
	}
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "placeholder.example.com\n", nil
	})
	enabled := true
	if _, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Enabled: &enabled, AckLicense: true}); err != nil {
		t.Fatal(err)
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(src.Key), []string{"bad.example"}); err != nil {
		t.Fatal(err)
	}
	if keys, _ := st.intel.AttributeDomain(ctx, "bad.example"); !strings.Contains(keys, src.Key) {
		t.Fatalf("初始归属应命中，got %q", keys)
	}

	// 改名：显示名需随索引重建更新。
	newName := "改名后的源"
	if _, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Name: &newName, AckLicense: true}); err != nil {
		t.Fatal(err)
	}
	if _, names := st.intel.AttributeDomain(ctx, "bad.example"); !strings.Contains(names, newName) {
		t.Fatalf("改名后显示名应更新，got %q", names)
	}

	// 停用：不再参与归属。
	disabled := false
	if _, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if keys, names := st.intel.AttributeDomain(ctx, "bad.example"); keys != "" || names != "" {
		t.Fatalf("停用后不应归属，got (%q,%q)", keys, names)
	}
}
