package ipam

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// OrgService 业务规则层：路径维护、环检测、删除保护。
type OrgService struct {
	store OrgStore
}

func NewOrgService(store OrgStore) *OrgService { return &OrgService{store: store} }

// Create 创建节点并计算物化路径；parentID 为空创建根。
func (s *OrgService) Create(_ context.Context, parentID, name string) (OrgNode, error) {
	id := uuid.NewString()
	if parentID != "" {
		if _, ok := s.store.Get(parentID); !ok {
			return OrgNode{}, ErrOrgNotFound
		}
	}
	parentPath := ""
	if parentID != "" {
		p, ok := s.store.Get(parentID)
		if !ok {
			return OrgNode{}, ErrOrgNotFound
		}
		parentPath = strings.TrimSuffix(p.Path, "/")
	}
	n := OrgNode{ID: id, ParentID: parentID, Name: name, Path: parentPath + "/" + id}
	if err := s.store.Create(n); err != nil {
		return OrgNode{}, err
	}
	return n, nil
}

// Update 改名与/或移动；移动到自身子孙返回 ErrOrgCycle，成功则级联刷新子树 path。
func (s *OrgService) Update(_ context.Context, id, newName, newParentID string, renameFlag, moveFlag bool) (OrgNode, error) {
	cur, ok := s.store.Get(id)
	if !ok {
		return OrgNode{}, ErrOrgNotFound
	}
	name := cur.Name
	if renameFlag {
		name = newName
	}
	parent := cur.ParentID
	if moveFlag {
		parent = newParentID
	}

	if moveFlag && parent == id {
		return OrgNode{}, ErrOrgCycle
	}
	if renameFlag {
		for _, n := range s.store.List() {
			same := n.ID == id
			if !same && n.ParentID == parent && strings.EqualFold(n.Name, name) {
				return OrgNode{}, ErrOrgNameDup
			}
		}
	}
	newParentPath := ""
	if moveFlag && parent != "" {
		p, ok := s.store.Get(parent)
		if !ok {
			return OrgNode{}, ErrOrgNotFound
		}
		if strings.HasPrefix(p.Path+"/", cur.Path+"/") {
			return OrgNode{}, ErrOrgCycle // 目标为自身子孙
		}
		newParentPath = strings.TrimSuffix(p.Path, "/")
	}

	next := OrgNode{ID: id, ParentID: parent, Name: name,
		Path: newParentPath + "/" + id}
	s.store.Update(next)
	s.rewriteSubtreePaths(next)
	return next, nil
}

// rewriteSubtreePaths 级联刷新子树物化路径（内存实现直改；PG 实现用单条前缀 UPDATE）。
func (s *OrgService) rewriteSubtreePaths(parent OrgNode) {
	for _, c := range s.store.List() {
		if c.ParentID == parent.ID {
			c.Path = parent.Path + "/" + c.ID
			s.store.Update(c)
			s.rewriteSubtreePaths(c)
		}
	}
}

// Delete 删除保护：子节点或资产引用即 ORG_IN_USE（§13.4）。
func (s *OrgService) Delete(_ context.Context, id string) error {
	if _, ok := s.store.Get(id); !ok {
		return ErrOrgNotFound
	}
	if s.store.HasChildren(id) || s.store.ReferencedByAsset(id) {
		return ErrOrgInUse
	}
	return s.store.Delete(id)
}

// Tree 全量构建嵌套树（listOrgTree 数据源）。
type TreeNode struct {
	Node
	Children []TreeNode
}

// Node 别名便于 JSON 复用 OrgNode 字段集。
type Node = OrgNode

func (s *OrgService) Tree(_ context.Context) []TreeNode {
	all := s.store.List()
	byParent := map[string][]OrgNode{}
	for _, n := range all {
		byParent[n.ParentID] = append(byParent[n.ParentID], n)
	}
	var build func(pid string) []TreeNode
	build = func(pid string) []TreeNode {
		out := []TreeNode{}
		for _, n := range byParent[pid] {
			out = append(out, TreeNode{Node: n, Children: build(n.ID)})
		}
		sortTree(out)
		return out
	}
	return build("")
}

func sortTree(ns []TreeNode) {
	for i := range ns {
		for j := i + 1; j < len(ns); j++ {
			if ns[j].Name < ns[i].Name {
				ns[i], ns[j] = ns[j], ns[i]
			}
		}
	}
}
