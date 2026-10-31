package platform

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session 登录会话（M6-007C）。仅保存元数据，不保存任何令牌原文；
// JWT 通过 sid claim 关联本表。强制下线 = revoked_at 置位 + 会话键入令牌黑名单。
type Session struct {
	ID         string     `json:"id"`
	UserID     string     `json:"userId"`
	Username   string     `json:"username"`
	IP         string     `json:"ip"`
	UserAgent  string     `json:"userAgent"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// SessionStore 会话持久化抽象（PG/内存双实现，沿库内 Store 惯例）。
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	ListByUser(ctx context.Context, userID string) ([]Session, error)
	ListAll(ctx context.Context, userID string) ([]Session, error)
	Get(ctx context.Context, id string) (Session, bool, error)
	// Touch 更新最近活动时间，并把过期时间推进到 max(expires_at, expires)。
	Touch(ctx context.Context, id string, at, expires time.Time) error
	Revoke(ctx context.Context, id string, at time.Time) error
	// RevokeByUser 注销该用户全部活跃会话，返回被注销的会话（供写黑名单）。
	RevokeByUser(ctx context.Context, userID string, at time.Time) ([]Session, error)
	PurgeExpired(ctx context.Context, before time.Time) error
}

// SessionKey 会话吊销键（前缀与 jti 区分；写入 auth_token_blacklist）。
func SessionKey(id string) string { return "sess:" + id }

// touchInterval 最近活动时间落库节流（每会话每分钟最多一次写）。
var touchInterval = time.Minute

// sessionPurgeInterval 过期会话清理周期（测试可覆写）。
var sessionPurgeInterval = time.Hour

// SessionService 会话登记/查询/强制下线的应用服务。
// store 为 nil 时退化为「仅黑名单」模式（单测与无 DB 的 PoC 路径）。
type SessionService struct {
	store SessionStore
	bl    *TokenBlacklist
	now   func() time.Time

	mu        sync.Mutex
	lastTouch map[string]time.Time
}

func NewSessionService(store SessionStore, bl *TokenBlacklist) *SessionService {
	return &SessionService{store: store, bl: bl, now: time.Now, lastTouch: map[string]time.Time{}}
}

// Start 启动过期会话清理协程（store 为 PG 实现时由 main 调用）。
func (s *SessionService) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	go func() {
		t := time.NewTicker(sessionPurgeInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				// 已过期超过 7 天的会话不再有取证价值；失败下周期重试。
				_ = s.store.PurgeExpired(ctx, s.now().Add(-7*24*time.Hour))
			}
		}
	}()
}

// NewSession 由登录用户记录生成会话（exp 为刷新令牌过期时刻）。
func (s *SessionService) NewSession(rec UserRecord, ip, ua string, exp time.Time) Session {
	now := s.now().UTC()
	return Session{
		ID: newJTI(), UserID: rec.ID, Username: rec.Username,
		IP: ip, UserAgent: truncate(ua, 512),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: exp.UTC(),
	}
}

// Create 持久化会话；失败仅记录日志（会话统计不阻断登录可用性）。
func (s *SessionService) Create(ctx context.Context, sess Session) {
	if s == nil || s.store == nil {
		return
	}
	if err := s.store.Create(ctx, sess); err != nil {
		log.Printf("[session] 会话登记失败（不影响登录）: %v", err)
	}
}

// Get 查询单个会话（不存在/存储不可用时返回 false）。
func (s *SessionService) Get(ctx context.Context, id string) (Session, bool) {
	if s == nil || s.store == nil || id == "" {
		return Session{}, false
	}
	sess, ok, err := s.store.Get(ctx, id)
	if err != nil {
		log.Printf("[session] 查询会话失败: %v", err)
		return Session{}, false
	}
	return sess, ok
}

// ListByUser 当前用户的活跃会话（最近活动倒序）。
func (s *SessionService) ListByUser(ctx context.Context, userID string) ([]Session, error) {
	if s == nil || s.store == nil {
		return []Session{}, nil
	}
	return s.store.ListByUser(ctx, userID)
}

// ListAll 管理端会话列表；userID 为空时返回全部用户。
func (s *SessionService) ListAll(ctx context.Context, userID string) ([]Session, error) {
	if s == nil || s.store == nil {
		return []Session{}, nil
	}
	return s.store.ListAll(ctx, userID)
}

// Touch 更新最近活动时间；按会话节流，写失败静默（不影响业务请求）。
func (s *SessionService) Touch(ctx context.Context, id string) {
	if s == nil || s.store == nil || id == "" {
		return
	}
	now := s.now()
	s.mu.Lock()
	last, ok := s.lastTouch[id]
	if ok && now.Sub(last) < touchInterval {
		s.mu.Unlock()
		return
	}
	if len(s.lastTouch) > 4096 {
		for k, v := range s.lastTouch {
			if now.Sub(v) > 10*touchInterval {
				delete(s.lastTouch, k)
			}
		}
	}
	s.lastTouch[id] = now
	s.mu.Unlock()
	if err := s.store.Touch(ctx, id, now, now.Add(refreshTokenLifetime)); err != nil {
		log.Printf("[session] 更新最近活动失败: %v", err)
	}
}

// Extend 刷新令牌轮换时同步推进会话过期时间（不受 Touch 节流影响）。
func (s *SessionService) Extend(ctx context.Context, id string, expires time.Time) {
	if s == nil || s.store == nil || id == "" {
		return
	}
	now := s.now()
	s.mu.Lock()
	s.lastTouch[id] = now
	s.mu.Unlock()
	if err := s.store.Touch(ctx, id, now, expires); err != nil {
		log.Printf("[session] 更新会话过期时间失败: %v", err)
	}
}

// Revoke 强制下线单个会话：DB 置吊销 + 会话键入黑名单至自然过期。
func (s *SessionService) Revoke(ctx context.Context, id string) error {
	if s == nil {
		return errors.New("会话服务未初始化")
	}
	sess, ok := s.Get(ctx, id)
	if !ok {
		return errSessionNotFound
	}
	if sess.RevokedAt != nil {
		return nil
	}
	if s.store != nil {
		if err := s.store.Revoke(ctx, id, s.now()); err != nil {
			return err
		}
	}
	s.addBlacklist(id, s.blacklistUntil(sess))
	return nil
}

// RevokeByUser 注销该用户全部活跃会话（改密/停用/重置 2FA/退出全部设备）。
func (s *SessionService) RevokeByUser(ctx context.Context, userID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	sessions, err := s.store.RevokeByUser(ctx, userID, s.now())
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		s.addBlacklist(sess.ID, s.blacklistUntil(sess))
	}
	return nil
}

// blacklistUntil 吊销键有效期：至少覆盖一个完整刷新令牌周期，
// 避免与刷新轮换并发时因会话过期时间被推进而出现吊销窗口漏洞。
func (s *SessionService) blacklistUntil(sess Session) time.Time {
	until := sess.ExpiresAt
	if min := s.now().Add(refreshTokenLifetime); min.After(until) {
		until = min
	}
	return until
}

func (s *SessionService) addBlacklist(id string, until time.Time) {
	if s.bl == nil || id == "" {
		return
	}
	s.bl.Add(SessionKey(id), until)
}

var errSessionNotFound = errors.New("会话不存在或已失效")

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ── 内存实现（单测/无 DB 路径） ──

type MemSessionStore struct {
	mu sync.Mutex
	m  map[string]Session
}

func NewMemSessionStore() *MemSessionStore { return &MemSessionStore{m: map[string]Session{}} }

func (s *MemSessionStore) Create(_ context.Context, sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[sess.ID] = sess
	return nil
}

func (s *MemSessionStore) ListByUser(_ context.Context, userID string) ([]Session, error) {
	return s.list(userID)
}

func (s *MemSessionStore) ListAll(_ context.Context, userID string) ([]Session, error) {
	return s.list(userID)
}

func (s *MemSessionStore) list(userID string) ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := []Session{}
	for _, v := range s.m {
		if v.RevokedAt != nil || !v.ExpiresAt.After(now) {
			continue
		}
		if userID != "" && v.UserID != userID {
			continue
		}
		out = append(out, v)
	}
	sortSessions(out)
	return out, nil
}

func (s *MemSessionStore) Get(_ context.Context, id string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	return v, ok, nil
}

func (s *MemSessionStore) Touch(_ context.Context, id string, at, expires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok {
		return nil
	}
	v.LastSeenAt = at
	if expires.After(v.ExpiresAt) {
		v.ExpiresAt = expires
	}
	s.m[id] = v
	return nil
}

func (s *MemSessionStore) Revoke(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok {
		return nil
	}
	if v.RevokedAt == nil {
		v.RevokedAt = &at
		s.m[id] = v
	}
	return nil
}

func (s *MemSessionStore) RevokeByUser(_ context.Context, userID string, at time.Time) ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Session{}
	for id, v := range s.m {
		if v.UserID != userID || v.RevokedAt != nil {
			continue
		}
		v.RevokedAt = &at
		s.m[id] = v
		out = append(out, v)
	}
	return out, nil
}

func (s *MemSessionStore) PurgeExpired(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, v := range s.m {
		if v.ExpiresAt.Before(before) {
			delete(s.m, id)
		}
	}
	return nil
}

// ── PostgreSQL 实现 ──

type PgSessionStore struct{ pool *pgxpool.Pool }

func NewPgSessionStore(pool *pgxpool.Pool) *PgSessionStore { return &PgSessionStore{pool: pool} }

const sessionCols = `id, user_id::text, username, ip, user_agent, created_at, last_seen_at, expires_at, revoked_at`

func scanSession(row pgx.Row) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.UserID, &s.Username, &s.IP, &s.UserAgent,
		&s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.RevokedAt)
	return s, err
}

func (s *PgSessionStore) Create(ctx context.Context, sess Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_sessions(id, user_id, username, ip, user_agent, created_at, last_seen_at, expires_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		sess.ID, sess.UserID, sess.Username, sess.IP, sess.UserAgent,
		sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt)
	return err
}

func (s *PgSessionStore) ListByUser(ctx context.Context, userID string) ([]Session, error) {
	return s.query(ctx, `user_id::text=$1`, userID)
}

func (s *PgSessionStore) ListAll(ctx context.Context, userID string) ([]Session, error) {
	if userID == "" {
		return s.query(ctx, `true`)
	}
	return s.query(ctx, `user_id::text=$1`, userID)
}

func (s *PgSessionStore) query(ctx context.Context, cond string, args ...any) ([]Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionCols+` FROM user_sessions
		 WHERE revoked_at IS NULL AND expires_at > now() AND `+cond+`
		 ORDER BY last_seen_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *PgSessionStore) Get(ctx context.Context, id string) (Session, bool, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx,
		`SELECT `+sessionCols+` FROM user_sessions WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	return sess, true, nil
}

func (s *PgSessionStore) Touch(ctx context.Context, id string, at, expires time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE user_sessions SET last_seen_at=$2, expires_at=GREATEST(expires_at,$3)
		 WHERE id=$1 AND revoked_at IS NULL`, id, at, expires)
	return err
}

func (s *PgSessionStore) Revoke(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE user_sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE id=$1`, id, at)
	return err
}

func (s *PgSessionStore) RevokeByUser(ctx context.Context, userID string, at time.Time) ([]Session, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE user_sessions SET revoked_at=$2
		 WHERE user_id::text=$1 AND revoked_at IS NULL AND expires_at > now()
		 RETURNING `+sessionCols, userID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *PgSessionStore) PurgeExpired(ctx context.Context, before time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_sessions WHERE expires_at < $1`, before)
	return err
}

func sortSessions(out []Session) {
	// 最近活动优先；同秒按创建时间与 ID 稳定排序。
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.LastSeenAt.Equal(b.LastSeenAt) {
			return a.LastSeenAt.After(b.LastSeenAt)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID < b.ID
	})
}
