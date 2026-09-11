package coherence

import "testing"

func TestMacFromDUID(t *testing.T) {
	// DUID-LLT（0001）：type(2)+hwtype(2)+time(4)+MAC(6)
	if got := macFromDUID("00:01:00:01:50:5c:ca:93:f8:e4:3b:e9:39:8d"); got != "f8e43be9398d" {
		t.Fatalf("LLT mac=%q", got)
	}
	// DUID-UUID（0004）不含 MAC
	if got := macFromDUID("00:04:95:61:26:c8:4e:7e:52:23:be:15:d0:a2:3b:13:b9:63"); got != "" {
		t.Fatalf("UUID should be empty, got %q", got)
	}
}

func TestIsDUIDForm(t *testing.T) {
	if !isDUIDForm("00:04:95:61:26:c8:4e:7e") {
		t.Fatal("00:04 应判为 DUID 形态")
	}
	if isDUIDForm("01:f8:e4:3b:e9:39:8d") {
		t.Fatal("01:MAC 不应判为 DUID 形态")
	}
}

func TestResolve_方案链顺序与各方式(t *testing.T) {
	l4 := []Lease4{
		{IPAddress: "10.0.0.15", HWAddress: "f8:e4:3b:e9:39:8d", Hostname: "cr-pc", State: 0},
		{IPAddress: "10.0.0.16", HWAddress: "aa:bb:cc:dd:ee:01", Hostname: "dup", State: 0},
		{IPAddress: "10.0.0.17", HWAddress: "aa:bb:cc:dd:ee:02", Hostname: "dup", State: 0},
		{IPAddress: "10.0.0.20", HWAddress: "11:22:33:44:55:66", ClientID: "00:04:aa:bb:cc:dd:ee:ff", Hostname: "rfc4361", State: 0},
	}
	idx := buildIdentityIndex(l4, []adminIdentity{{Mac: "999999999999", Duid: "0004001122334455"}})

	cases := []struct {
		name   string
		v6     Lease6
		wantM  string
		wantW  string
		reason string
	}{
		{"admin 权威优先", Lease6{DUID: "00:04:00:11:22:33:44:55", HWAddress: "f8:e4:3b:e9:39:8d", Hostname: "cr-pc"}, "999999999999", "admin", ""},
		{"option79 次优先", Lease6{DUID: "00:04:95:61:26:c8:4e:7e", HWAddress: "f8:e4:3b:e9:39:8d", Hostname: "cr-pc"}, "f8e43be9398d", "option79", ""},
		{"client-id 桥", Lease6{DUID: "00:04:aa:bb:cc:dd:ee:ff", Hostname: "rfc4361"}, "112233445566", "client-id", ""},
		{"duid-llt 提取", Lease6{DUID: "00:01:00:01:50:5c:ca:93:aa:bb:cc:dd:ee:01"}, "aabbccddee01", "duid-llt", ""},
		{"hostname 唯一", Lease6{DUID: "00:04:ff", Hostname: "cr-pc."}, "f8e43be9398d", "hostname", ""},
		{"hostname 同名歧义", Lease6{DUID: "00:04:ee", Hostname: "dup"}, "", "", "ambiguous_hostname"},
		{"无信号", Lease6{DUID: "00:04:dd", Hostname: "nobody"}, "", "", "no_mac_signal"},
	}
	for _, c := range cases {
		mac, method, reason := idx.resolve(c.v6)
		if mac != c.wantM || method != c.wantW || reason != c.reason {
			t.Fatalf("%s: got mac=%q method=%q reason=%q want mac=%q method=%q reason=%q",
				c.name, mac, method, reason, c.wantM, c.wantW, c.reason)
		}
	}
}

func TestSchemeAllows_钉死拦截(t *testing.T) {
	if !schemeAllows("auto", "hostname") || !schemeAllows("", "option79") {
		t.Fatal("auto/空 应接受任意方式")
	}
	if !schemeAllows("option79", "option79") {
		t.Fatal("钉死同方式应通过")
	}
	if schemeAllows("option79", "hostname") {
		t.Fatal("钉死 option79 时 hostname 回落应被拦")
	}
}
