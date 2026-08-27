package logquery

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ActorType 调用者类型（§12.3：审计区分人/AI）。
type ActorType string

const (
	ActorHuman  ActorType = "human"
	ActorBot    ActorType = "bot"
	ActorSystem ActorType = "system"
)

// AuditEntry 单条操作审计。
type AuditEntry struct {
	ID        int64
	TS        time.Time
	ActorType ActorType
	Actor     string
	TokenSub  string
	Method    string
	Path      string
	Action    string
	Resource  string
	Status    int
	Detail    string
}

// AuditFilter /audits 过滤条件。
type AuditFilter struct {
	From      time.Time
	To        time.Time
	ActorType ActorType
	Action    string
	Q         string // resource/path 子串
	Cursor    string // {tsMillis}:{id}
	PageSize  int
}

// AuditPage 审计分页。
type AuditPage struct {
	Items      []AuditEntry
	NextCursor string
	Total      int
}

// AuditStore 审计持久化抽象（PG/内存双实现）。
type AuditStore interface {
	Append(ctx context.Context, e AuditEntry) (AuditEntry, error)
	Query(ctx context.Context, f AuditFilter) (AuditPage, error)
}

// EncodeAuditCursor 游标："{tsUnixMilli}:{id}"。
func EncodeAuditCursor(e AuditEntry) string {
	return fmt.Sprintf("%d:%d", e.TS.UTC().UnixMilli(), e.ID)
}

// ParseAuditCursor 解析游标；非法返回零值 ts。
func ParseAuditCursor(s string) (ts time.Time, id int64) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0
	}
	if id, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return time.Time{}, 0
	}
	return time.UnixMilli(ms).UTC(), id
}

// lessAuditDesc 排序键 (ts DESC, id DESC)。
func lessAuditDesc(a, b AuditEntry) bool {
	if !a.TS.Equal(b.TS) {
		return a.TS.After(b.TS)
	}
	return a.ID > b.ID
}

// auditAfterCursor 行严格位于游标之后（DESC 序）。
func auditAfterCursor(r AuditEntry, cts time.Time, cid int64) bool {
	rt, ct := r.TS.UTC().UnixMilli(), cts.UTC().UnixMilli()
	if rt != ct {
		return rt < ct
	}
	return r.ID < cid
}

// MemAuditStore 内存实现（PoC/单测）。
type MemAuditStore struct {
	mu   sync.Mutex
	seq  int64
	rows []AuditEntry
}

func NewMemAuditStore() *MemAuditStore { return &MemAuditStore{} }

func (m *MemAuditStore) Append(_ context.Context, e AuditEntry) (AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	e.ID = m.seq
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	if e.Actor == "" {
		e.Actor = "anonymous"
	}
	m.rows = append(m.rows, e)
	return e, nil
}

func (m *MemAuditStore) Query(_ context.Context, f AuditFilter) (AuditPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := make([]AuditEntry, 0, len(m.rows))
	for _, r := range m.rows {
		if auditMatch(r, f) {
			filtered = append(filtered, r)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return lessAuditDesc(filtered[i], filtered[j]) })
	total := len(filtered)

	if f.Cursor != "" {
		cts, cid := ParseAuditCursor(f.Cursor)
		if !cts.IsZero() {
			kept := filtered[:0]
			for _, r := range filtered {
				if auditAfterCursor(r, cts, cid) {
					kept = append(kept, r)
				}
			}
			filtered = kept
		}
	}

	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPage
	}
	page := filtered
	next := ""
	if len(filtered) > pageSize {
		page = filtered[:pageSize]
		next = EncodeAuditCursor(filtered[pageSize-1])
	}
	return AuditPage{Items: page, NextCursor: next, Total: total}, nil
}

func auditMatch(r AuditEntry, f AuditFilter) bool {
	if r.TS.Before(f.From) || (!f.To.IsZero() && r.TS.After(f.To)) {
		return false
	}
	if f.ActorType != "" && r.ActorType != f.ActorType {
		return false
	}
	if f.Action != "" && r.Action != f.Action {
		return false
	}
	if f.Q != "" {
		needle := strings.ToLower(f.Q)
		return strings.Contains(strings.ToLower(r.Resource), needle) ||
			strings.Contains(strings.ToLower(r.Path), needle)
	}
	return true
}
