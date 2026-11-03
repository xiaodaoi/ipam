package dashboard

import "testing"

// TestGenPoolTopOrgFields 校验池利用率行携带所属部门字段（M6-020）。
func TestGenPoolTopOrgFields(t *testing.T) {
	rows := []PoolUtil{
		{SubnetID: "44444444-4444-4444-4444-444444444444", Name: "研发", CIDR: "10.0.0.0/24",
			OrgID: "org-1", OrgName: "研发中心", PoolIdx: 0, Used: 3, Capacity: 10, Pct: 30},
		{SubnetID: "55555555-5555-5555-5555-555555555555", Name: "无组织", CIDR: "10.0.1.0/24",
			PoolIdx: 0, Used: 1, Capacity: 10, Pct: 10},
	}
	got := genPoolTop(rows)
	if got == nil || len(*got) != 2 {
		t.Fatalf("genPoolTop rows=%v", got)
	}
	first := (*got)[0]
	if first.OrgId == nil || *first.OrgId != "org-1" {
		t.Fatalf("orgId 应为 org-1: %+v", first.OrgId)
	}
	if first.OrgName == nil || *first.OrgName != "研发中心" {
		t.Fatalf("orgName 应为 研发中心: %+v", first.OrgName)
	}
	// 组织缺失时字段应省略（nil），前端显示占位。
	second := (*got)[1]
	if second.OrgId != nil || second.OrgName != nil {
		t.Fatalf("空组织应省略字段: %+v %+v", second.OrgId, second.OrgName)
	}
}
