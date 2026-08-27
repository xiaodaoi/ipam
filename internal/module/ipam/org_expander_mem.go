package ipam

import (
	"context"
	"strings"

	"github.com/xiaodaoi/ipam/internal/module/logquery"
)

// MemOrgExpander logquery.OrgExpander 的内存实现（PoC 模式）：
// 子树展开走 OrgStore 节点遍历（PG 侧用 org_group.path 物化路径）。
type MemOrgExpander struct {
	orgs   OrgStore
	subs   SubnetRepo
	assets AssetRepo
}

func NewMemOrgExpander(orgs OrgStore, subs SubnetRepo, assets AssetRepo) *MemOrgExpander {
	return &MemOrgExpander{orgs: orgs, subs: subs, assets: assets}
}

// Expand 展开 orgID 子树的 CIDR ∪ 资产 MAC。
func (e *MemOrgExpander) Expand(ctx context.Context, orgID string) (logquery.OrgScope, error) {
	if _, ok := e.orgs.Get(orgID); !ok {
		return logquery.OrgScope{}, logquery.ErrOrgNotFound
	}
	ids := subtreeIDs(e.orgs.List(), orgID)
	var scope logquery.OrgScope
	subs, err := e.subs.List(ctx, "", 0)
	if err != nil {
		return scope, err
	}
	idset := map[string]bool{}
	for _, id := range ids {
		idset[id] = true
	}
	for _, s := range subs {
		if idset[s.OrgID] && s.CIDR != "" {
			scope.CIDRs = append(scope.CIDRs, s.CIDR)
		}
	}
	list, err := e.assets.List(ctx, "", "")
	if err != nil {
		return scope, err
	}
	for _, a := range list {
		if idset[a.OrgID] && a.MAC != "" {
			scope.MACs = append(scope.MACs, a.MAC)
		}
	}
	return scope, nil
}

// subtreeIDs BFS 收集子树节点 ID（含自身；MemOrgStore 无物化路径查询能力）。
func subtreeIDs(nodes []OrgNode, root string) []string {
	children := map[string][]string{}
	for _, n := range nodes {
		if n.ParentID != "" {
			children[n.ParentID] = append(children[n.ParentID], strings.Clone(n.ID))
		}
	}
	out := []string{root}
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, children[cur]...)
		queue = append(queue, children[cur]...)
	}
	return out
}
