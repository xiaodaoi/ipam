package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TokenBlacklist 已吊销令牌（M5-011：登出即吊销）。内存实现：
// 条目 TTL = 令牌剩余寿命（自然过期自清）；进程重启清空（PoC 语义，重启后
// 存量令牌仍有效至自然过期——多实例/持久化黑名单为后续卡）。
type TokenBlacklist struct {
	mu sync.Mutex
	m  map[string]time.Time // tokenHash → 过期时刻
	db *pgxpool.Pool        // 非 nil 时 Add 双写 DB（M5-012）
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
	now := time.Now()
	for k, v := range b.m {
		if now.After(v) {
			delete(b.m, k)
		}
	}
	b.m[hash] = until
	db := b.db
	b.mu.Unlock()
	if db != nil {
		// best-effort 双写：失败仅影响重启恢复窗口（罕见故障可接受），内存仍即时生效
		_, _ = db.Exec(context.Background(),
			`INSERT INTO auth_token_blacklist (token_hash, until) VALUES ($1, $2)
			 ON CONFLICT (token_hash) DO UPDATE SET until = EXCLUDED.until`, hash, until)
	}
}

// Revoked 是否已被吊销（M5-031 两级检查）：
//  1. 本地内存——本实例登出的令牌立即生效，免 DB 往返；
//  2. miss 且 db 非 nil 时 PG 点查——其他实例登出的令牌全局立即生效（多实例语义）。
//
// PG 查询失败吞错返回 false：fail-open 窗口内后续 users 查询与业务写同处 PG 故障域必然失败，
// 等效 fail-closed（不为此增加错误上报面）。
func (b *TokenBlacklist) Revoked(hash string) bool {
	b.mu.Lock()
	until, ok := b.m[hash]
	if ok && time.Now().After(until) {
		delete(b.m, hash)
		ok = false
	}
	db := b.db
	b.mu.Unlock()
	if ok {
		return true
	}
	if db != nil {
		var exists bool
		_ = db.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM auth_token_blacklist WHERE token_hash=$1 AND until > now())`,
			hash).Scan(&exists)
		return exists
	}
	return false
}

// Len 当前黑名单条数（诊断用）。
func (b *TokenBlacklist) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

// AttachDB 启用黑名单持久化（M5-012）：清理过期条目 + 加载未过期条目进内存；
// 之后 Add 双写 DB，Revoked 仍走内存（启动加载保证重启后有效，单实例语义）。
func (b *TokenBlacklist) AttachDB(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM auth_token_blacklist WHERE until <= now()`); err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `SELECT token_hash, until FROM auth_token_blacklist WHERE until > now()`)
	if err != nil {
		return err
	}
	defer rows.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	for rows.Next() {
		var hash string
		var until time.Time
		if err := rows.Scan(&hash, &until); err != nil {
			return err
		}
		b.m[hash] = until
	}
	if err := rows.Err(); err != nil {
		return err
	}
	b.db = pool
	return nil
}
