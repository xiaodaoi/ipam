package dualstack

import (
	"context"
	"testing"
)

func TestMemStoreCRUD(t *testing.T) {
	s := NewMemStore()
	created, err := s.Create(context.Background(), Template{
		Name: "办公池A", V4Cidr: "192.168.0.0/24", V6Prefix: "2407::/64",
		Encoding: "B", Expr: "{v4.hextet4}", DnsSync: true, GraceHours: 24, Enabled: true,
	})
	if err != nil || created.ID == "" {
		t.Fatalf("create: %v", err)
	}
	list, _ := s.List(context.Background())
	if len(list) != 1 || list[0].Name != "办公池A" {
		t.Fatalf("list: %+v", list)
	}
	if err := s.Delete(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List(context.Background())
	if len(list) != 0 {
		t.Fatalf("delete 后应空")
	}
}

func TestCoherenceProjectionSkipsDisabled(t *testing.T) {
	ts := []Template{
		{ID: "a", V4Cidr: "10.0.0.0/24", V6Prefix: "2407::/64", Expr: "{v4.hextet4}", Enabled: true},
		{ID: "b", V4Cidr: "10.0.1.0/24", V6Prefix: "2408::/64", Expr: "{v4.hextet4}", Enabled: false},
	}
	proj := CoherenceTemplates(ts)
	if len(proj) != 1 || proj[0].ID != "a" {
		t.Fatalf("disabled 应被投影剔除: %+v", proj)
	}
}
