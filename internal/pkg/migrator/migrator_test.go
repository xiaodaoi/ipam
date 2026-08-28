package migrator

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestListFilesSortedSkipsNonSQL(t *testing.T) {
	fsys := fstest.MapFS{
		"0010_users.sql":  {Data: []byte("CREATE TABLE IF NOT EXISTS users(id int);")},
		"0002_notify.sql": {Data: []byte("SELECT 1;")},
		"0001_init.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS t0(id int);")},
		"README.md":       {Data: []byte("skip me")},
		"sub":             {Mode: fs.ModeDir},
	}
	files, err := listFiles(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("应只取 3 个 .sql: %d", len(files))
	}
	want := []string{"0001_init", "0002_notify", "0010_users"}
	for i, f := range files {
		if f.version != want[i] {
			t.Fatalf("顺序 %d: got %s want %s", i, f.version, want[i])
		}
	}
}

func TestVersionPrefix(t *testing.T) {
	// 回归：字典序下 "0009_x" > "0009"，基线比较必须用数字前缀
	cases := map[string]string{
		"0009_operation_audit": "0009",
		"0010_users":           "0010",
		"0001_init":            "0001",
		"bare":                 "bare",
	}
	for in, want := range cases {
		if got := versionPrefix(in); got != want {
			t.Fatalf("versionPrefix(%s)=%s want %s", in, got, want)
		}
	}
}

func TestComputePending(t *testing.T) {
	files := []migrationFile{
		{version: "0001_init"}, {version: "0009_operation_audit"},
		{version: "0010_users"}, {version: "0011_future"},
	}
	// 全新库：无记账、非基线 → 全量执行
	p := computePending(files, map[string]bool{}, false)
	if len(p) != 4 {
		t.Fatalf("全新库应全量: %d", len(p))
	}
	// 存量库：基线命中 → ≤0009 跳过
	p = computePending(files, map[string]bool{}, true)
	if len(p) != 2 || p[0].version != "0010_users" || p[1].version != "0011_future" {
		t.Fatalf("存量基线应只剩 0010+: %+v", p)
	}
	// 正常增量：记账含 0010 → 只剩 0011
	p = computePending(files, map[string]bool{"0001_init": true, "0009_operation_audit": true, "0010_users": true}, false)
	if len(p) != 1 || p[0].version != "0011_future" {
		t.Fatalf("增量应只剩 0011: %+v", p)
	}
	// 全部已应用 → 空
	p = computePending(files, map[string]bool{"0001_init": true, "0009_operation_audit": true, "0010_users": true, "0011_future": true}, false)
	if len(p) != 0 {
		t.Fatalf("全部已应用应为空: %+v", p)
	}
}
