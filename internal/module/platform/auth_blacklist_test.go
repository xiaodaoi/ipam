package platform

import (
	"testing"
	"time"
)

func TestTokenBlacklist_语义(t *testing.T) {
	bl := NewTokenBlacklist()
	h := TokenHash("tok-1")
	if bl.Revoked(h) {
		t.Fatal("未加入不应命中")
	}
	bl.Add(h, time.Now().Add(time.Hour))
	if !bl.Revoked(h) {
		t.Fatal("加入后应命中")
	}
	if bl.Len() != 1 {
		t.Fatalf("len=%d", bl.Len())
	}
	if bl.Revoked(TokenHash("tok-2")) {
		t.Fatal("不同令牌不应误伤")
	}
	// 过期条目惰性清除
	bl.Add(h, time.Now().Add(-time.Minute))
	if bl.Revoked(h) {
		t.Fatal("过期后不应命中")
	}
	if bl.Len() != 0 {
		t.Fatalf("过期应清除: %d", bl.Len())
	}
}

func TestTokenHash_确定性(t *testing.T) {
	// 同输入两次调用结果一致（赋值变量避免 SA4000 字面比较误报）
	h1 := TokenHash("a")
	h2 := TokenHash("a")
	if h1 != h2 {
		t.Fatal("应确定")
	}
	if h1 == TokenHash("b") {
		t.Fatal("不同输入应不同哈希")
	}
}
