package ipam

import (
	"strings"
	"testing"
)

// TestAssetWhereIndexFriendly（M6-064）：检索条件必须与 0056 的 GIN 索引表达式一致。
func TestAssetWhereIndexFriendly(t *testing.T) {
	var args []any
	w := assetWhere(AssetQuery{
		OrgIDs: []string{"00000000-0000-0000-0000-000000000001"},
		Q:      "aabbccdd",
	}, &args)
	if strings.Contains(w, "org_id::text") {
		t.Fatalf("org 过滤使用 ::text 会绕过索引: %s", w)
	}
	if !strings.Contains(w, "org_id = ANY(") {
		t.Fatalf("org 过滤未使用 uuid[] ANY: %s", w)
	}
	if strings.Contains(w, "coalesce(mac") || strings.Contains(w, "coalesce(owner") {
		t.Fatalf("关键字过滤包裹 coalesce 会使 trgm 索引失效: %s", w)
	}
	if !strings.Contains(w, "mac ILIKE") || !strings.Contains(w, "owner ILIKE") {
		t.Fatalf("关键字过滤表达式不符: %s", w)
	}
	if !strings.Contains(w, "replace(lower(mac), ':', '')") {
		t.Fatalf("紧凑 MAC 过滤表达式不符: %s", w)
	}
}
