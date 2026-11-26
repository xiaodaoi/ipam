package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestWriteAtomic_并发不产生混合内容（M6-052）：固定临时文件名会让两个
// 并发 apply 互相截断，最终文件可能是「A 的前缀 + B 的尾部」。
// 唯一临时文件 + rename 必须保证落盘内容始终等于某一次完整输入。
func TestWriteAtomic_并发不产生混合内容(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "unbound.conf")

	const workers = 24
	payloads := make([]string, workers)
	for i := range payloads {
		// 每个 payload 内部结构可区分，混合内容会被下面的完整性校验发现
		payloads[i] = strings.Repeat(string(rune('a'+i)), 64*1024) + "\n# tail-" + string(rune('A'+i)) + "\n"
	}
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = writeAtomic(dst, payloads[i])
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, p := range payloads {
		if string(got) == p {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("落盘内容不是任何一次完整写入（长度=%d），疑似并发混合", len(got))
	}
	// 不应残留临时文件
	left, _ := filepath.Glob(filepath.Join(dir, "unbound.conf.*.tmp"))
	if len(left) != 0 {
		t.Fatalf("残留临时文件: %v", left)
	}
}
