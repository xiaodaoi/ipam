package ipam

import (
	"net/http"

	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// OrgHandler 实现 apigen.ServerInterface 中 org 域端点（§13.4 主数据）。
type OrgHandler struct {
	svc *OrgService
}

func NewOrgHandler(svc *OrgService) *OrgHandler { return &OrgHandler{svc: svc} }

func toGenNode(n OrgNode) apigen.OrgNode {
	parent := n.ParentID
	return apigen.OrgNode{Id: rtypes.UUID(guuid.MustParse(n.ID)), ParentId: &parent, Name: n.Name, Path: n.Path}
}

func toGenTree(nodes []TreeNode) []apigen.OrgTreeNode {
	out := make([]apigen.OrgTreeNode, 0, len(nodes))
	for _, n := range nodes {
		children := toGenTree(n.Children)
		parent := n.ParentID
		out = append(out, apigen.OrgTreeNode{
			Id: rtypes.UUID(guuid.MustParse(n.ID)), ParentId: &parent, Name: n.Name, Path: n.Path,
			Children: children,
		})
	}
	return out
}

// ListOrgTree GET /orgs（spec operationId listOrgTree；前端统一走 /orgs）
func (h *OrgHandler) ListOrgTree(c *gin.Context) {
	tree := h.svc.Tree(c.Request.Context())
	c.JSON(http.StatusOK, toGenTree(tree))
}

// CreateOrg POST /orgs
func (h *OrgHandler) CreateOrg(c *gin.Context) {
	var body apigen.OrgCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	n, err := h.svc.Create(c.Request.Context(), derefStr(body.ParentId), body.Name)
	if err != nil {
		mapErr(c, err)
		return
	}
	g := toGenNode(n)
	c.JSON(http.StatusCreated, g)
}

// UpdateOrg PATCH /orgs/{orgId}
func (h *OrgHandler) UpdateOrg(c *gin.Context, orgId apigen.OrgId) {
	var body apigen.OrgUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	rename := body.Name != nil
	move := body.ParentId != nil
	name := ""
	newParent := ""
	if rename {
		name = *body.Name
	}
	if move {
		newParent = derefStr(body.ParentId)
	}
	n, err := h.svc.Update(c.Request.Context(), guuid.UUID(orgId).String(), name, newParent, rename, move)
	if err != nil {
		mapErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenNode(n))
}

// DeleteOrg DELETE /orgs/{orgId}
func (h *OrgHandler) DeleteOrg(c *gin.Context, orgId apigen.OrgId) {
	if err := h.svc.Delete(c.Request.Context(), guuid.UUID(orgId).String()); err != nil {
		mapErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func mapErr(c *gin.Context, err error) {
	switch err.Error() {
	case ErrOrgNotFound.Error():
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ORG_NOT_FOUND", "组织节点不存在")
	case ErrOrgNameDup.Error():
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/org-name-dup", "ORG_NAME_DUP", "同父下已存在同名分组")
	case ErrOrgCycle.Error():
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/org-cycle", "ORG_CYCLE", "移动目标为自身或其子孙")
	case ErrOrgInUse.Error():
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/org-in-use", "ORG_IN_USE", "该分组下存在子节点或仍被子网/资产引用")
	default:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
