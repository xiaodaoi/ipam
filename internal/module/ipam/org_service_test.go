package ipam

import (
	"context"
	"strings"
	"testing"
)

func newTreeSvc(t *testing.T) (*OrgService, *MemOrgStore) {
	t.Helper()
	s := NewMemOrgStore()
	svc := NewOrgService(s)
	ctx := context.Background()
	root, err := svc.Create(ctx, "", "A公司")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, root.ID, "研发部"); err != nil {
		t.Fatal(err)
	}
	return svc, s
}

func TestCreate_路径与嵌套(t *testing.T) {
	_, store := newTreeSvc(t)
	nodes := store.List()
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.Name == "研发部" {
			root, _ := store.Get(n.ParentID)
			if !strings.HasPrefix(n.Path, root.Path+"/") {
				t.Fatalf("child path %q not under root %q", n.Path, root.Path)
			}
		}
	}
}

func TestCreate_同父重名(t *testing.T) {
	svc, _ := newTreeSvc(t)
	if _, err := svc.Create(context.Background(), "", "A公司"); err != ErrOrgNameDup {
		t.Fatalf("err=%v want ErrOrgNameDup", err)
	}
}

func TestDelete_子节点保护(t *testing.T) {
	svc, store := newTreeSvc(t)
	var rootID string
	for _, n := range store.List() {
		if n.Name == "A公司" {
			rootID = n.ID
		}
	}
	if err := svc.Delete(context.Background(), rootID); err != ErrOrgInUse {
		t.Fatalf("err=%v want ErrOrgInUse", err)
	}
}

func TestDelete_资产引用保护(t *testing.T) {
	svc, store := newTreeSvc(t)
	var leaf OrgNode
	for _, n := range store.List() {
		if n.Name == "研发部" {
			leaf = n
		}
	}
	store.SeedAsset("aa:bb:cc:dd:ee:01", leaf.ID)
	if err := svc.Delete(context.Background(), leaf.ID); err != ErrOrgInUse {
		t.Fatalf("err=%v want ErrOrgInUse(asset)", err)
	}
}

func TestUpdate_移动环检测(t *testing.T) {
	svc, store := newTreeSvc(t)
	ctx := context.Background()
	var rootID, childID string
	for _, n := range store.List() {
		switch n.Name {
		case "A公司":
			rootID = n.ID
		case "研发部":
			childID = n.ID
		}
	}
	if _, err := svc.Update(ctx, rootID, "", childID, false, true); err != ErrOrgCycle {
		t.Fatalf("err=%v want ErrOrgCycle", err)
	}
}

func TestUpdate_同父改名冲突(t *testing.T) {
	svc, store := newTreeSvc(t)
	ctx := context.Background()
	root, _ := svc.Create(ctx, "", "根2")
	dev, err := svc.Create(ctx, root.ID, "研发部")
	if err != nil {
		t.Fatal(err)
	}
	test_, err := svc.Create(ctx, root.ID, "测试部")
	if err != nil {
		t.Fatal(err)
	}
	_ = store

	var devID, testID string
	for _, n := range store.List() {
		switch n.Name {
		case dev.Name:
			devID = n.ID
		case test_.Name:
			testID = n.ID
		}
	}
	if _, err := svc.Update(ctx, testID, "研发部", "", true, false); err != ErrOrgNameDup {
		t.Fatalf("err=%v want ErrOrgNameDup", err)
	}
	_ = devID
}

func TestUpdate_移动级联刷新路径(t *testing.T) {
	svc, store := newTreeSvc(t)
	ctx := context.Background()
	newRoot, err := svc.Create(ctx, "", "B公司")
	if err != nil {
		t.Fatal(err)
	}
	var childID string
	for _, n := range store.List() {
		if n.Name == "研发部" {
			childID = n.ID
		}
	}
	moved, err := svc.Update(ctx, childID, "", newRoot.ID, false, true)
	if err != nil {
		t.Fatal(err)
	}
	nr, _ := store.Get(newRoot.ID)
	if !strings.HasPrefix(moved.Path, nr.Path+"/") {
		t.Fatalf("moved path %q not under new root %q", moved.Path, nr.Path)
	}
}

func TestTree_嵌套排序(t *testing.T) {
	svc, _ := newTreeSvc(t)
	tree := svc.Tree(context.Background())
	if len(tree) != 1 || tree[0].Name != "A公司" || len(tree[0].Children) != 1 {
		t.Fatalf("unexpected tree: %+v", tree)
	}
}

func TestOrgService_Reorder同级排序与树序(t *testing.T) {
	ctx := context.Background()
	s := NewMemOrgStore()
	svc := NewOrgService(s)
	// 建根 A/B/C（字母序自动 B C A... 测试重排效果）
	ids := []string{}
	for _, name := range []string{"A公司", "B公司", "C公司"} {
		n, err := svc.Create(ctx, "", name)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	// 默认按字母序：A,B,C → A公司,B公司,C公司
	tree := svc.Tree(ctx)
	if tree[0].Name != "A公司" || tree[2].Name != "C公司" {
		t.Fatalf("初始字母序不符: %s,%s,%s", tree[0].Name, tree[1].Name, tree[2].Name)
	}
	// 重排为 C,B,A
	if err := svc.Reorder(ctx, "", []string{ids[2], ids[1], ids[0]}); err != nil {
		t.Fatal(err)
	}
	tree = svc.Tree(ctx)
	if tree[0].Name != "C公司" || tree[1].Name != "B公司" || tree[2].Name != "A公司" {
		t.Fatalf("重排后顺序不符: %s,%s,%s", tree[0].Name, tree[1].Name, tree[2].Name)
	}
	// 不同父节点混排 → ORG_MOVE
	child, err := svc.Create(ctx, ids[0], "研发部")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Reorder(ctx, "", []string{child.ID, ids[1]}); err != ErrOrgMove {
		t.Fatalf("跨父重排 err=%v want ORG_MOVE", err)
	}
	// 追加节点排在末尾
	n, _ := svc.Create(ctx, "", "D公司")
	_ = n
	tree = svc.Tree(ctx)
	if tree[3].Name != "D公司" {
		t.Fatalf("新建应追加末尾: %+v", tree)
	}
}

func TestCreate_名称规范化与校验(t *testing.T) {
	svc := NewOrgService(NewMemOrgStore())
	ctx := context.Background()

	// 首尾空白（半角/全角/制表）应被去除
	for _, raw := range []string{" 研发部 ", "\u3000运维部\u3000", "\t安全部\n"} {
		n, err := svc.Create(ctx, "", raw)
		if err != nil {
			t.Fatalf("create %q: %v", raw, err)
		}
		if strings.TrimSpace(n.Name) != n.Name || n.Name == "" {
			t.Fatalf("名称未规范化: %q -> %q", raw, n.Name)
		}
	}

	// 空名称 / 纯空白
	for _, raw := range []string{"", "   ", "\u3000", "\t\n"} {
		if _, err := svc.Create(ctx, "", raw); err != ErrOrgNameMissing {
			t.Fatalf("create %q err=%v want ErrOrgNameMissing", raw, err)
		}
	}

	// 长度边界：64 字符通过，65 拒绝（按 rune 计）
	ok64 := strings.Repeat("组", 64)
	if _, err := svc.Create(ctx, "", ok64); err != nil {
		t.Fatalf("64 字符应通过: %v", err)
	}
	if _, err := svc.Create(ctx, "", strings.Repeat("组", 65)); err != ErrOrgNameTooLong {
		t.Fatalf("65 字符 err=%v want ErrOrgNameTooLong", err)
	}
}

func TestCreate_根节点与兄弟判重大小写不敏感(t *testing.T) {
	svc := NewOrgService(NewMemOrgStore())
	ctx := context.Background()
	root, err := svc.Create(ctx, "", "HQ")
	if err != nil {
		t.Fatal(err)
	}
	// 根节点（parent 为空）同名
	if _, err := svc.Create(ctx, "", "hq"); err != ErrOrgNameDup {
		t.Fatalf("同名根节点 err=%v want ErrOrgNameDup", err)
	}
	// 子节点同名
	if _, err := svc.Create(ctx, root.ID, "Ops"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, root.ID, "  ops "); err != ErrOrgNameDup {
		t.Fatalf("同名子节点 err=%v want ErrOrgNameDup", err)
	}
	// 不同父可同名
	other, err := svc.Create(ctx, "", "Branch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, other.ID, "ops"); err != nil {
		t.Fatalf("不同父应允许同名: %v", err)
	}
}

func TestUpdate_改名校验(t *testing.T) {
	svc, _ := newTreeSvc(t)
	ctx := context.Background()
	var id string
	for _, n := range svc.store.List() {
		if n.Name == "研发部" {
			id = n.ID
		}
	}
	if id == "" {
		t.Fatal("未找到测试节点")
	}
	if _, err := svc.Update(ctx, id, "   ", "", true, false); err != ErrOrgNameMissing {
		t.Fatalf("空名 err=%v want ErrOrgNameMissing", err)
	}
	if _, err := svc.Update(ctx, id, strings.Repeat("x", 65), "", true, false); err != ErrOrgNameTooLong {
		t.Fatalf("超长 err=%v want ErrOrgNameTooLong", err)
	}
	n, err := svc.Update(ctx, id, "  平台组  ", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "平台组" {
		t.Fatalf("改名未规范化: %q", n.Name)
	}
}
