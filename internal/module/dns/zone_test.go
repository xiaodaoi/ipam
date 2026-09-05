package dns

import (
	"context"
	"strings"
	"testing"
)

func TestValidateRecord_类型语法(t *testing.T) {
	if err := ValidateRecord("A", "10.1.2.3"); err != nil {
		t.Fatalf("valid A rejected: %v", err)
	}
	if err := ValidateRecord("A", "not-an-ip"); err == nil {
		t.Fatal("invalid A accepted")
	}
	if err := ValidateRecord("AAAA", "2406::1"); err != nil {
		t.Fatalf("valid AAAA rejected: %v", err)
	}
	if err := ValidateRecord("CNAME", "target.example.com."); err != nil {
		t.Fatalf("valid CNAME rejected: %v", err)
	}
	if err := ValidateRecord("CNAME", "target.example.com"); err == nil {
		t.Fatal("CNAME without trailing dot accepted")
	}
}

func TestExportZonefile_格式(t *testing.T) {
	zone := Zone{ID: "z1", Name: "corp.local.", Kind: "auth"}
	records := []Record{
		{Name: "www", RecType: "A", TTL: 300, Rdata: "10.61.172.80", Enabled: true},
		{Name: "host-aa", RecType: "AAAA", TTL: 0, Rdata: "2406::1", Enabled: true},
		{Name: "disabled.x", RecType: "A", TTL: 300, Rdata: "10.0.0.1", Enabled: false},
	}
	out := ExportZonefile(zone, records)
	if !strings.Contains(out, "$ORIGIN corp.local.\n") {
		t.Fatalf("missing origin:\n%s", out)
	}
	if !strings.Contains(out, "www.corp.local. 300 IN A 10.61.172.80\n") {
		t.Fatalf("missing A record:\n%s", out)
	}
	if strings.Contains(out, "disabled.x") {
		t.Fatalf("disabled record exported:\n%s", out)
	}
}

func TestZoneService_变更触发ApplyConf(t *testing.T) {
	fc := &fakeCtl{}
	svc := NewZoneService(NewMemZoneRepo(), fc)
	applies := 0
	svc.ApplyConf = func(context.Context) error { applies++; return nil }
	zone, err := svc.CreateZone(context.Background(), Zone{Name: "test.local.", Kind: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if applies != 1 {
		t.Fatalf("CreateZone 应触发 ApplyConf，实际 %d", applies)
	}
	if _, err := svc.CreateRecord(context.Background(), zone.ID, Record{Name: "www", RecType: "A", TTL: 300, Rdata: "10.1.1.1"}); err != nil {
		t.Fatal(err)
	}
	if applies != 2 {
		t.Fatalf("CreateRecord 应触发 ApplyConf，实际 %d", applies)
	}
	// 同名同类型冲突
	if _, err := svc.CreateRecord(context.Background(), zone.ID, Record{Name: "www", RecType: "A", TTL: 300, Rdata: "10.1.1.2"}); err != ErrRecordNameDup {
		t.Fatalf("err=%v want RECORD_NAME_DUP", err)
	}
}

func TestZoneService_非法记录拒绝且不触发Apply(t *testing.T) {
	svc := NewZoneService(NewMemZoneRepo(), &fakeCtl{})
	applies := 0
	svc.ApplyConf = func(context.Context) error { applies++; return nil }
	zone, _ := svc.CreateZone(context.Background(), Zone{Name: "t.local.", Kind: "auth"})
	applies = 0
	if _, err := svc.CreateRecord(context.Background(), zone.ID, Record{Name: "x", RecType: "A", Rdata: "bad"}); err == nil {
		t.Fatal("want BAD_RDATA")
	}
	if applies != 0 {
		t.Fatal("invalid record must not trigger apply")
	}
}
