package coherence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteSnapshotAtomic_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "bindings.snapshot")

	want := []Binding{
		{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.61.172.10", IPv6: "2406::10:61:172:10", TemplateID: "t-b", Hostname: "printer"},
		{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.0.0.5", IPv6: "2406::5"},
	}
	if err := WriteSnapshotAtomic(path, want); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{
		"# ipam bindings.snapshot v2",
		"aa:bb:cc:dd:ee:01|10.61.172.10|2406::10:61:172:10|t-b|printer",
		"aa:bb:cc:dd:ee:02|10.0.0.5|2406::5||",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing line %q in:\n%s", want, text)
		}
	}
}

func TestWriteSnapshotAtomic_NoTmpLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.snapshot")
	if err := WriteSnapshotAtomic(path, []Binding{{MAC: "m"}}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || len(e.Name()) > len("b.snapshot")+2 {
			t.Fatalf("tmp residue: %s", e.Name())
		}
	}
}

func TestStartSnapshotLoop_RefreshesWithin5s(t *testing.T) {
	store := NewMemStore()
	path := filepath.Join(t.TempDir(), "b.snapshot")
	stop := StartSnapshotLoop(path, 50*time.Millisecond, store.All)
	defer stop()

	time.Sleep(20 * time.Millisecond) // 首刷
	store.Put(Binding{MAC: "x1", IPv4: "1.2.3.4", IPv6: "::1"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "x1") {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("snapshot not refreshed within deadline")
}
