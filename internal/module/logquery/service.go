package logquery

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	MaxWindow     = 31 * 24 * time.Hour
	DefaultPage   = 100
	DefaultLimit  = 10
	DefaultBucket = 60
	MaxPage       = 500
)

var (
	ErrWindowTooLarge = errors.New("时间窗超过 31 天上限")
	ErrInvalidCursor  = errors.New("游标格式非法")
	ErrMissingFrom    = errors.New("from 时间参数必填")
	ErrOrgNotFound    = errors.New("组织不存在")
)

// Service 日志检索编排：校验 → 组织展开 → Store。
type Service struct {
	store    Store
	expander OrgExpander
}

func NewService(store Store, expander OrgExpander) *Service {
	return &Service{store: store, expander: expander}
}

// Query /logs 组合过滤 + 游标分页。
func (s *Service) Query(ctx context.Context, f LogFilter) (Page, error) {
	if err := s.validateWindow(f.From, f.To); err != nil {
		return Page{}, err
	}
	if f.PageSize <= 0 {
		f.PageSize = DefaultPage
	}
	if f.PageSize > MaxPage {
		f.PageSize = MaxPage
	}
	if f.MAC != "" {
		f.MAC = NormalizeMAC(f.MAC)
	}
	if f.Page < 0 {
		f.Page = 0
	}
	if f.Cursor != "" {
		cts, _, _ := ParseCursor(f.Cursor)
		if cts.IsZero() {
			return Page{}, ErrInvalidCursor
		}
	}
	scope, err := s.expand(ctx, f.OrgID)
	if err != nil {
		return Page{}, err
	}
	return s.store.Query(ctx, f, scope)
}

// Top /logs/top TopN 域名或客户端。
func (s *Service) Top(ctx context.Context, q TopQuery) ([]TopEntry, int, error) {
	if err := s.validateWindow(q.From, q.To); err != nil {
		return nil, 0, err
	}
	if q.By != "client" {
		q.By = "domain"
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	scope, err := s.expand(ctx, q.OrgID)
	if err != nil {
		return nil, 0, err
	}
	return s.store.Top(ctx, q, scope)
}

// Qps /logs/qps 时序曲线。
func (s *Service) Qps(ctx context.Context, q QpsQuery) ([]QpsPoint, error) {
	if err := s.validateWindow(q.From, q.To); err != nil {
		return nil, err
	}
	if q.IntervalSec <= 0 {
		q.IntervalSec = DefaultBucket
	}
	scope, err := s.expand(ctx, q.OrgID)
	if err != nil {
		return nil, err
	}
	return s.store.Qps(ctx, q, scope)
}

// validateWindow from 必填且窗口 ≤31 天；to 零值=当前时间。
func (s *Service) validateWindow(from, to time.Time) error {
	if from.IsZero() {
		return ErrMissingFrom
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if to.Sub(from) > MaxWindow {
		return ErrWindowTooLarge
	}
	return nil
}

// expand 组织展开；OrgID 为空返回空 scope（无过滤）。
func (s *Service) expand(ctx context.Context, orgID string) (OrgScope, error) {
	if orgID == "" || s.expander == nil {
		return OrgScope{}, nil
	}
	return s.expander.Expand(ctx, orgID)
}

// NormalizeMAC 归一化为 12 位小写 hex（去冒号/短横线；Vector 关联键格式）。
func NormalizeMAC(mac string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(mac) {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			b.WriteRune(c)
		}
	}
	return b.String()
}
