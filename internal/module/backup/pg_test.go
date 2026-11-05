package backup

import (
	"strings"
	"testing"
)

// TestPgDumpArgs_ExcludesMetricData M6-032：指标数据行不得进入备份，
// 否则换机器恢复后历史趋势会显示「上一台机器」的 CPU/磁盘读数。
func TestPgDumpArgs_ExcludesMetricData(t *testing.T) {
	r := &PgCommandRunner{host: "db", port: 5432, user: "ipam", db: "ipam"}
	args := strings.Join(r.dumpArgs("/tmp/x.dump"), " ")

	for _, want := range []string{
		"--exclude-table-data=system_metric",
		"--exclude-table-data=system_disk_metric",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("pg_dump 参数缺少 %q: %s", want, args)
		}
	}
	// 表结构仍要保留（恢复后迁移与查询不依赖备份内容）。
	if strings.Contains(args, "--exclude-table=") {
		t.Fatalf("不应整表排除（会丢表结构）: %s", args)
	}
}
