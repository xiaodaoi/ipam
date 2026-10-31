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

// blacklistKey 黑名单条目键（M5-032 起 jti 为主键）；存量无 jti 令牌回退令牌指纹，
// 保证升级前签发令牌的登出吊销与 DB 旧行查询仍然命中。
func blacklistKey(claims JWTClaims, rawToken string) string {
	if claims.JTI != "" {
		return claims.JTI
	}
	return TokenHash(rawToken)
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
	return b.RevokedAny(hash)
}

// RevokedAny 批量吊销检查（M6-007C）：一次 DB 查询同时判断令牌 jti 与会话键，
// 避免每个请求为会话状态额外增加一次往返。
func (b *TokenBlacklist) RevokedAny(keys ...string) bool {
	if b == nil || len(keys) == 0 {
		return false
	}
	now := time.Now()
	b.mu.Lock()
	hit := false
	valid := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if until, ok := b.m[k]; ok {
			if now.After(until) {
				delete(b.m, k)
				continue
			}
			hit = true
		}
		valid = append(valid, k)
	}
	db := b.db
	b.mu.Unlock()
	if hit {
		return true
	}
	if db != nil && len(valid) > 0 {
		var exists bool
		_ = db.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM auth_token_blacklist WHERE token_hash = ANY($1) AND until > now())`,
			valid).Scan(&exists)
		return exists
	}
	return false
}

// revocationKeys 请求令牌的吊销检查键：令牌 jti（存量令牌回退指纹）+ 会话键（M6-007C）。
func revocationKeys(claims JWTClaims, rawToken string) []string {
	keys := []string{blacklistKey(claims, rawToken)}
	if claims.SID != "" {
		keys = append(keys, SessionKey(claims.SID))
	}
	return keys
}

// Len 当前黑名单条数（诊断用）。
func (b *TokenBlacklist) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

// blacklistPurgeInterval 运行期过期行清理周期（M5-032；测试可覆写）。
var blacklistPurgeInterval = time.Hour

// AttachDB 启用黑名单持久化（M5-012）：清理过期条目 + 加载未过期条目进内存；
// 之后 Add 双写 DB，Revoked 仍走内存（启动加载保证重启后有效，单实例语义）。
// M5-032：自启运行期每小时清理协程（ctx 取消即退出）。
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
	go purgeExpiredLoop(ctx, pool)
	return nil
}

// purgeExpiredLoop 每小时清理 auth_token_blacklist 已过期行（M5-032：长运行防表膨胀）。
// 清理失败静默重试下一周期——表增长缓慢，偶发失败可接受；ctx 取消即退出。
func purgeExpiredLoop(ctx context.Context, pool *pgxpool.Pool) {
	t := time.NewTicker(blacklistPurgeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = pool.Exec(ctx, `DELETE FROM auth_token_blacklist WHERE until <= now()`)
		}
	}
}
