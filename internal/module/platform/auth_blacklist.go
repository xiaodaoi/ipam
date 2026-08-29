package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// TokenBlacklist 已吊销令牌（M5-011：登出即吊销）。内存实现：
// 条目 TTL = 令牌剩余寿命（自然过期自清）；进程重启清空（PoC 语义，重启后
// 存量令牌仍有效至自然过期——多实例/持久化黑名单为后续卡）。
type TokenBlacklist struct {
	mu sync.Mutex
	m  map[string]time.Time // tokenHash → 过期时刻
}

func NewTokenBlacklist() *TokenBlacklist {
	return &TokenBlacklist{m: map[string]time.Time{}}
}

// TokenHash 令牌指纹（SHA256 hex）——黑名单不存原始令牌。
func TokenHash(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// Add 吊销令牌至其自然过期时刻（写入前惰性清理已过期条目）。
func (b *TokenBlacklist) Add(hash string, until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, v := range b.m {
		if now.After(v) {
			delete(b.m, k)
		}
	}
	b.m[hash] = until
}

// Revoked 是否已被吊销（命中且未过期；过期条目惰性清除）。
func (b *TokenBlacklist) Revoked(hash string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until, ok := b.m[hash]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(b.m, hash)
		return false
	}
	return true
}

// Len 当前黑名单条数（诊断用）。
func (b *TokenBlacklist) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}
