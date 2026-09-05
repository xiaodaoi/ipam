// Package ipam 承载地址管理业务域（FR-A）。
package ipam

import (
	"errors"
)

// OrgNode 组织分组节点（PG org_group 行投影，§13.4 主数据）。
type OrgNode struct {
	ID        string
	ParentID  string // 空=根
	Name      string
	Path      string // 物化路径 /<id>/<id>
	SortOrder int    // 同级排序（组织拖拽排序，0020 迁移）
}

// OrgStore 组织持久化抽象：M2-002 起提供 pgx 实现；当前内存实现支撑 PoC。
type OrgStore interface {
	List() []OrgNode
	Get(id string) (OrgNode, bool)
	Create(OrgNode) error
	Update(OrgNode)
	Delete(id string) error
	// HasChildren / ReferencedByAsset 删除保护探测；referenced 钩子供未来子网等表扩展。
	HasChildren(id string) bool
	ReferencedByAsset(id string) bool
}

var (
	ErrOrgNotFound = errors.New("ORG_NOT_FOUND")
	ErrOrgNameDup  = errors.New("ORG_NAME_DUP")
	ErrOrgInUse    = errors.New("ORG_IN_USE")
	ErrOrgCycle    = errors.New("ORG_CYCLE")
	ErrOrgMove     = errors.New("ORG_MOVE")
)
