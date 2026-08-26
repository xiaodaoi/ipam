package ipam

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// MemOrgStore 内存实现（PoC/单测）；并发安全。
type MemOrgStore struct {
	mu     sync.RWMutex
	nodes  map[string]OrgNode
	assets map[string]string // mac -> org_id（模拟 asset 引用，删除保护用）
}

func NewMemOrgStore() *MemOrgStore {
	return &MemOrgStore{nodes: map[string]OrgNode{}, assets: map[string]string{}}
}

// SeedAsset 模拟资产引用（测试与 PoC 预置）。
func (s *MemOrgStore) SeedAsset(mac, orgID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[mac] = orgID
}

func (s *MemOrgStore) List() []OrgNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]OrgNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (s *MemOrgStore) Get(id string) (OrgNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	return n, ok
}

func (s *MemOrgStore) Create(n OrgNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := uuid.Parse(n.ID); err != nil {
		return fmt.Errorf("bad id: %w", err)
	}
	if _, dup := s.byNameLocked(n.ParentID, n.Name); dup {
		return ErrOrgNameDup
	}
	s.nodes[n.ID] = n
	return nil
}

func (s *MemOrgStore) Update(n OrgNode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[n.ID] = n
}

func (s *MemOrgStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	return nil
}

func (s *MemOrgStore) HasChildren(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.nodes {
		if n.ParentID == id {
			return true
		}
	}
	return false
}

func (s *MemOrgStore) ReferencedByAsset(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, oid := range s.assets {
		if oid == id {
			return true
		}
	}
	return false
}

func (s *MemOrgStore) byNameLocked(parentID, name string) (OrgNode, bool) {
	for _, n := range s.nodes {
		if n.ParentID == parentID && strings.EqualFold(n.Name, name) {
			return n, true
		}
	}
	return OrgNode{}, false
}
