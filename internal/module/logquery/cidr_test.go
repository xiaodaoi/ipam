package logquery

import (
	"net/netip"
	"testing"
)

// M2-032 出口回归：v6 池 → dashboard poolTop → mappedV6(纯 v6) 曾因 As4 panic（dashboard 500）。
func TestMappedV6_纯v6原样不panic(t *testing.T) {
	v6 := netip.MustParseAddr("2406:172::100:0")
	got := mappedV6(v6)
	if got != netip.AddrFrom16(v6.As16()) {
		t.Fatalf("v6 should round-trip as-is, got %v", got)
	}
}

func TestMappedV6_v4映射形态(t *testing.T) {
	got := mappedV6(netip.MustParseAddr("10.0.0.1"))
	want := netip.AddrFrom16([16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 0, 0, 1})
	if got != want {
		t.Fatalf("v4 mapped mismatch: got %v want %v", got, want)
	}
}
