package dns

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
)

// ForwardRule 条件转发规则（PG forward_rule 行投影）。
type ForwardRule struct {
	ID          string
	Domain      string // 域名后缀；"." 为默认
	UpstreamIDs []string
	Enabled     bool
	Note        string
}

// ForwardRuleRepo 持久化。
type ForwardRuleRepo interface {
	List(ctx context.Context) ([]ForwardRule, error)
	Create(ctx context.Context, r ForwardRule) (ForwardRule, error)
	Update(ctx context.Context, r ForwardRule) error
	Delete(ctx context.Context, id string) error
}

var ErrForwardRuleNotFound = errors.New("FORWARD_RULE_NOT_FOUND")

// MatchDomain 最长后缀匹配：返回命中的规则；无命中返回 nil。
func MatchDomain(rules []ForwardRule, qname string) *ForwardRule {
	q := strings.ToLower(strings.TrimSuffix(qname, "."))
	var best *ForwardRule
	bestLen := -1
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			continue
		}
		d := strings.ToLower(strings.TrimSuffix(r.Domain, "."))
		if d == "." || d == "" {
			// 默认转发兜底：任何域名皆匹配，优先级最低
			if bestLen < 0 {
				best, bestLen = r, 0
			}
			continue
		}
		if q == d || strings.HasSuffix(q, "."+d) {
			if len(d) > bestLen {
				best, bestLen = r, len(d)
			}
		}
	}
	return best
}

// ForwardService 转发规则业务。
type ForwardService struct {
	repo ForwardRuleRepo
	ups  UpstreamRepo
	ctl  UnboundController
}

func NewForwardService(repo ForwardRuleRepo, ups UpstreamRepo, ctl UnboundController) *ForwardService {
	return &ForwardService{repo: repo, ups: ups, ctl: ctl}
}

// Create dryRun=true 仅生成命令预览。
func (s *ForwardService) Create(ctx context.Context, r ForwardRule, dryRun bool) (ForwardRule, []string, error) {
	if err := s.validateUpstreams(ctx, r.UpstreamIDs); err != nil {
		return ForwardRule{}, nil, err
	}
	if dryRun {
		cmds, err := s.previewWith(ctx, r)
		return r, cmds, err
	}
	saved, err := s.repo.Create(ctx, r)
	if err != nil {
		return ForwardRule{}, nil, err
	}
	if err := s.sync(ctx); err != nil {
		return saved, nil, ErrUnboundDown
	}
	return saved, nil, nil
}

// previewWith 现库规则 + 候选规则合成预览（dryRun 场景）。
func (s *ForwardService) previewWith(ctx context.Context, candidate ForwardRule) ([]string, error) {
	rules, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	if candidate.Domain != "" {
		rules = append(rules, candidate)
	}
	ups, err := s.ups.List(ctx)
	if err != nil {
		return nil, err
	}
	return buildForwardCommands(rules, ups), nil
}

func (s *ForwardService) sync(ctx context.Context) error {
	rules, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	ups, err := s.ups.List(ctx)
	if err != nil {
		return err
	}
	return s.ctl.SyncForwardRules(ctx, rules, ups)
}

func (s *ForwardService) validateUpstreams(ctx context.Context, ids []string) error {
	ups, err := s.ups.List(ctx)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, u := range ups {
		have[u.ID] = true
	}
	for _, id := range ids {
		if !have[id] {
			return ErrUpstreamNotFound
		}
	}
	return nil
}

// buildForwardCommands 生成 unbound-control 命令串（与 ExecController.SyncForwardRules 一致）。
func buildForwardCommands(rules []ForwardRule, ups []Upstream) []string {
	byID := map[string]Upstream{}
	for _, u := range ups {
		byID[u.ID] = u
	}
	cmds := []string{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		args := []string{"forward_add", r.Domain}
		for _, id := range r.UpstreamIDs {
			u, ok := byID[id]
			if !ok || !u.Enabled {
				continue
			}
			args = append(args, u.Addrs...)
		}
		if len(args) > 2 {
			cmds = append(cmds, strings.Join(args, " "))
		}
	}
	return cmds
}

// MemForwardRuleRepo 内存实现。
type MemForwardRuleRepo struct {
	mu    sync.Mutex
	items map[string]ForwardRule
	seq   int
}

func NewMemForwardRuleRepo() *MemForwardRuleRepo {
	return &MemForwardRuleRepo{items: map[string]ForwardRule{}}
}

func (r *MemForwardRuleRepo) List(_ context.Context) ([]ForwardRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []ForwardRule{}
	for _, v := range r.items {
		out = append(out, v)
	}
	return out, nil
}
func (r *MemForwardRuleRepo) Create(_ context.Context, fr ForwardRule) (ForwardRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	fr.ID = "fr-" + strconv.Itoa(r.seq)
	r.items[fr.ID] = fr
	return fr, nil
}
func (r *MemForwardRuleRepo) Update(_ context.Context, fr ForwardRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[fr.ID]; !ok {
		return ErrForwardRuleNotFound
	}
	r.items[fr.ID] = fr
	return nil
}
func (r *MemForwardRuleRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
	return nil
}
