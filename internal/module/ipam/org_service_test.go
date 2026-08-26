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
