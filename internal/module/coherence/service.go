package coherence

import (
	"context"
	"sync"

	coherencev1 "github.com/xiaodaoi/ipam/proto/gen/coherence/v1"
)

// Binding 绑定台账最小投影（PG coherence_binding 行）。
type Binding struct {
	MAC        string
	IPv4       string
	IPv6       string // 已计算/缓存结果；空=待算
	TemplateID string
	Hostname   string
}

// Store 台账存取抽象：M1-004 换 PG 实现，当前内存热集。
type Store interface {
	Get(mac string) (Binding, bool)
	Put(b Binding)
	Delete(mac string)
}

type MemStore struct {
	mu sync.RWMutex
	m  map[string]Binding
}

func NewMemStore() *MemStore { return &MemStore{m: map[string]Binding{}} }

func (s *MemStore) Get(mac string) (Binding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.m[mac]
	return b, ok
}

func (s *MemStore) Put(b Binding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[b.MAC] = b
}

func (s *MemStore) Delete(mac string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, mac)
}

// TemplateLookup 模板查询抽象。
type TemplateLookup func(id string) (Template, bool)

// Service 实现 proto Coherence 服务（§2.1 时序）。
type Service struct {
	coherencev1.UnimplementedCoherenceServer
	store       Store
	templates   TemplateLookup
	templateAll func() []Template // 多池对全量；用于按 IPv4 最长前缀自动选模板
}

func NewService(store Store, templates TemplateLookup) *Service {
	return &Service{store: store, templates: templates}
}

// SetTemplateAll 注入多池对全量模板（§4.3 自动匹配；daemon 从 PG prefix_template 装载）。
func (s *Service) SetTemplateAll(fn func() []Template) *Service {
	s.templateAll = fn
	return s
}

// ResolveBinding：缓存优先(CACHE)，未算则按模板现算(COMPUTED)，无绑定返回 NONE。
// 模板选择：优先绑定记录中的 TemplateID；缺省时按 IPv4 最长前缀自动匹配（多池对）。
func (s *Service) ResolveBinding(_ context.Context, req *coherencev1.ResolveRequest) (*coherencev1.ResolveResponse, error) {
	b, ok := s.store.Get(req.GetMac())
	if !ok {
		return &coherencev1.ResolveResponse{Hit: false, Source: coherencev1.ResolveResponse_NONE}, nil
	}
	if b.IPv6 != "" {
		return resp(true, b.IPv6, b.TemplateID, coherencev1.ResolveResponse_CACHE), nil
	}
	var tpl Template
	matched := false
	if b.TemplateID != "" {
		if t, ok2 := s.templates(b.TemplateID); ok2 {
			tpl, matched = t, true
		}
	}
	if !matched && s.templateAll != nil {
		if t, err := MatchIPv4Template(s.templateAll(), b.IPv4); err == nil {
			tpl, matched = t, true
			b.TemplateID = tpl.ID
		}
	}
	if !matched {
		return &coherencev1.ResolveResponse{Hit: false, Source: coherencev1.ResolveResponse_NONE}, nil
	}
	ip6, err := ApplyTemplate(tpl, b.IPv4)
	if err != nil {
		return &coherencev1.ResolveResponse{Hit: false, Source: coherencev1.ResolveResponse_NONE}, nil
	}
	b.IPv6 = ip6
	s.store.Put(b)
	return resp(true, ip6, b.TemplateID, coherencev1.ResolveResponse_COMPUTED), nil
}

// ReportLease：commit/renew 落台账；expire/release 删除（生命周期 §4.3，grace 状态机 M2 扩展）。
func (s *Service) ReportLease(_ context.Context, req *coherencev1.LeaseReport) (*coherencev1.Ack, error) {
	switch req.GetEvent() {
	case coherencev1.LeaseEvent_COMMIT, coherencev1.LeaseEvent_RENEW:
		s.store.Put(Binding{
			MAC: NormalizeMAC(req.GetMac()), IPv4: req.GetIpv4(), IPv6: req.GetIpv6(),
			TemplateID: "", Hostname: req.GetHostname(),
		})
	case coherencev1.LeaseEvent_EXPIRE, coherencev1.LeaseEvent_RELEASE:
		s.store.Delete(req.GetMac())
	default:
		return &coherencev1.Ack{Ok: false, Message: "unknown event"}, nil
	}
	return &coherencev1.Ack{Ok: true}, nil
}

func resp(hit bool, ipv6, tplID string, src coherencev1.ResolveResponse_Source) *coherencev1.ResolveResponse {
	return &coherencev1.ResolveResponse{Hit: hit, Ipv6: ipv6, TemplateId: tplID, Source: src}
}

// Has 供测试与对账探活使用。
func (s *MemStore) Has(mac string) bool {
	_, ok := s.Get(mac)
	return ok
}

// All 全量导出（快照与对账循环使用）。
func (s *MemStore) All() []Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Binding, 0, len(s.m))
	for _, b := range s.m {
		out = append(out, b)
	}
	return out
}
