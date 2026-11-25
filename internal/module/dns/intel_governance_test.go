package dns

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// M6-039 P2：同步观测、单源贡献上限、版本快照与一键回滚。

func TestIntelSource_同步记录与版本回滚(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()

	src, err := st.intel.CreateSource(ctx, IntelSource{
		Name: "版本源", URL: "https://feed.example/list.txt", Layer: "hot",
	})
	if err != nil {
		t.Fatal(err)
	}
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "a.example.com\nb.example.com\nc.example.com\n", nil
	})
	enabled := true
	got, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Enabled: &enabled, AckLicense: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries != 3 || got.LastAdded != 3 || got.LastRemoved != 0 {
		t.Fatalf("首次同步统计不符: entries=%d added=%d removed=%d", got.Entries, got.LastAdded, got.LastRemoved)
	}

	runs, err := st.intel.SourceRuns(ctx, src.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "ok" || runs[0].Trigger != "enable" || runs[0].Revision != 1 {
		t.Fatalf("首次执行记录不符: %+v", runs)
	}

	// 第二次：a 保留，b/c 移除，d 新增。
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "a.example.com\nd.example.com\n", nil
	})
	got, err = st.intel.SyncSource(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries != 2 || got.LastAdded != 1 || got.LastRemoved != 2 {
		t.Fatalf("二次同步差异不符: entries=%d added=%d removed=%d", got.Entries, got.LastAdded, got.LastRemoved)
	}
	revs, err := st.intel.SourceRevisions(ctx, src.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Revision != 2 || !revs[0].Active || revs[1].Revision != 1 || revs[1].Active {
		t.Fatalf("版本列表不符: %+v", revs)
	}

	// 回滚到 v1：b/c 恢复，d 消失。
	got, err = st.intel.RollbackRevision(ctx, src.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries != 3 || got.LastAdded != 2 || got.LastRemoved != 1 {
		t.Fatalf("回滚统计不符: entries=%d added=%d removed=%d", got.Entries, got.LastAdded, got.LastRemoved)
	}
	zone := st.zone(t, ZoneHot)
	for _, d := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		if !strings.Contains(zone, d) {
			t.Fatalf("回滚后热层缺少 %s:\n%s", d, zone)
		}
	}
	if strings.Contains(zone, "d.example.com") {
		t.Fatalf("回滚后热层不应保留 d.example.com:\n%s", zone)
	}
	revs, err = st.intel.SourceRevisions(ctx, src.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !revs[1].Active || revs[0].Active {
		t.Fatalf("回滚后生效版本应为 v1: %+v", revs)
	}
	runs, err = st.intel.SourceRuns(ctx, src.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].Status != "rolled_back" || runs[0].Trigger != "rollback" || runs[0].Revision != 1 {
		t.Fatalf("回滚执行记录不符: %+v", runs[0])
	}

	// 原 v2 仍在，可再次回滚（可逆）。
	if _, err := st.intel.RollbackRevision(ctx, src.ID, 2); err != nil {
		t.Fatal(err)
	}
	if z := st.zone(t, ZoneHot); !strings.Contains(z, "d.example.com") || strings.Contains(z, "b.example.com") {
		t.Fatalf("回滚到 v2 结果不符:\n%s", z)
	}

	// 不存在的版本。
	if _, err := st.intel.RollbackRevision(ctx, src.ID, 99); err != ErrRevisionNotFound {
		t.Fatalf("不存在的版本应报 ErrRevisionNotFound，got=%v", err)
	}
}

func TestIntelSource_内容未变不生成新版本(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()

	src, err := st.intel.CreateSource(ctx, IntelSource{Name: "稳定源", URL: "https://feed.example/x.txt", Layer: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	body := "x.example.com\ny.example.com\n"
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) { return body, nil })
	enabled := true
	if _, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Enabled: &enabled, AckLicense: true}); err != nil {
		t.Fatal(err)
	}
	got, err := st.intel.SyncSource(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastAdded != 0 || got.LastRemoved != 0 {
		t.Fatalf("内容未变不应有差异: added=%d removed=%d", got.LastAdded, got.LastRemoved)
	}
	revs, _ := st.intel.SourceRevisions(ctx, src.ID, 10)
	if len(revs) != 1 {
		t.Fatalf("内容未变不应生成新版本，实际 %d 个", len(revs))
	}
}

func TestIntelSource_白名单排除计数(t *testing.T) {
	st := newIntelStack(t)
	st.intel.SetSecretDir(t.TempDir())
	ctx := context.Background()
	if _, err := st.store.CreateWhitelist(ctx, IntelWhitelist{
		Kind: "suffix", Value: "good.example.com", Reason: "误报", Source: "manual", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	src, err := st.intel.CreateSource(ctx, IntelSource{Name: "含白名单源", URL: "https://feed.example/x.txt", Layer: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	st.intel.SetSourceFetcher(func(context.Context, string, string, string) (string, error) {
		return "bad.example.com\nsub.good.example.com\nother.example.net\n", nil
	})
	enabled := true
	got, err := st.intel.UpdateSource(ctx, src.ID, SourceUpdate{Enabled: &enabled, AckLicense: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries != 3 || got.LastExcluded != 1 {
		t.Fatalf("白名单排除统计不符: entries=%d excluded=%d", got.Entries, got.LastExcluded)
	}
	runs, _ := st.intel.SourceRuns(ctx, src.ID, 10)
	if len(runs) == 0 || runs[0].Excluded != 1 {
		t.Fatalf("执行记录未带排除数: %+v", runs)
	}
	if z := st.zone(t, ZoneHot); strings.Contains(z, "sub.good.example.com") {
		t.Fatalf("白名单子域不应进入热层:\n%s", z)
	}
}

func TestIntelSource_单源贡献上限(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()

	// 两个源，各 60000 条（合计 120000 > 热层上限 100000），各限 60%。
	mk := func(prefix string, n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("%s%d.example.com", prefix, i))
		}
		return out
	}
	a, err := st.intel.CreateSource(ctx, IntelSource{Name: "A", URL: "https://a.example/x", Layer: "hot", MaxSharePct: 60})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.intel.CreateSource(ctx, IntelSource{Name: "B", URL: "https://b.example/x", Layer: "hot", MaxSharePct: 60})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(a.Key), mk("a", 60000)); err != nil {
		t.Fatal(err)
	}
	if err := writeDomainCache(st.intel.SourceDomainPath(b.Key), mk("b", 60000)); err != nil {
		t.Fatal(err)
	}
	a.Enabled, b.Enabled = true, true
	recs := st.intel.feedRecordsForLayer([]IntelSource{a, b}, "hot", map[string]bool{})
	if len(recs) != MaxHotEntries {
		t.Fatalf("应按层容量截断到 %d，实际 %d", MaxHotEntries, len(recs))
	}
	counts := map[byte]int{}
	for _, r := range recs {
		if len(r.Name) > 0 {
			counts[r.Name[0]]++
		}
	}
	if counts['a'] != 60000 {
		t.Fatalf("A 源应恰好贡献 60000 条，实际 %d", counts['a'])
	}
	if counts['b'] != MaxHotEntries-60000 {
		t.Fatalf("B 源应被全局容量截断到 %d 条，实际 %d", MaxHotEntries-60000, counts['b'])
	}

	// 总需求未超过容量时不做单源限制（两个小源都应完整进入）。
	smallA, _ := st.intel.CreateSource(ctx, IntelSource{Name: "sA", URL: "https://sa.example/x", Layer: "broad", MaxSharePct: 10})
	smallB, _ := st.intel.CreateSource(ctx, IntelSource{Name: "sB", URL: "https://sb.example/x", Layer: "broad", MaxSharePct: 10})
	_ = writeDomainCache(st.intel.SourceDomainPath(smallA.Key), []string{"s1.example.com", "s2.example.com"})
	_ = writeDomainCache(st.intel.SourceDomainPath(smallB.Key), []string{"s3.example.com", "s4.example.com"})
	smallA.Enabled, smallB.Enabled = true, true
	got := st.intel.feedRecordsForLayer([]IntelSource{smallA, smallB}, "broad", map[string]bool{})
	if len(got) != 4 {
		t.Fatalf("未超容量时不应截断，实际 %d 条", len(got))
	}
}
