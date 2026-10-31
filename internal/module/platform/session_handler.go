package platform

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// SessionHandler 实现会话管理端点（M6-007C）：
//   - GET/DELETE /auth/sessions[/{sessionId}]  当前账号自助查看/下线
//   - GET/DELETE /system/sessions[/{sessionId}] 管理员查看/强制下线（system 域 RBAC）
type SessionHandler struct {
	svc *SessionService
}

func NewSessionHandler(svc *SessionService) *SessionHandler {
	return &SessionHandler{svc: svc}
}

// toGenSessions 投影为 API 形状；currentSID 标记当前请求所属会话。
func toGenSessions(items []Session, currentSID string) []apigen.Session {
	out := make([]apigen.Session, 0, len(items))
	for _, s := range items {
		out = append(out, apigen.Session{
			Id: s.ID, UserId: s.UserID, Username: s.Username,
			Ip: s.IP, UserAgent: s.UserAgent,
			CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt,
			Current: s.ID == currentSID,
		})
	}
	return out
}

// claimsOf 解析当前请求令牌；失败时已写 401。
func claimsOf(c *gin.Context) (JWTClaims, bool) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return JWTClaims{}, false
	}
	return claims, true
}

// ListAuthSessions GET /auth/sessions
func (h *SessionHandler) ListAuthSessions(c *gin.Context) {
	claims, ok := claimsOf(c)
	if !ok {
		return
	}
	items, err := h.svc.ListByUser(c.Request.Context(), claims.UID)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, apigen.SessionList{Items: toGenSessions(items, claims.SID)})
}

// RevokeAuthSession DELETE /auth/sessions/{sessionId}
func (h *SessionHandler) RevokeAuthSession(c *gin.Context, sessionId string) {
	claims, ok := claimsOf(c)
	if !ok {
		return
	}
	sess, found := h.svc.Get(c.Request.Context(), sessionId)
	if !found || sess.UserID != claims.UID {
		WriteProblem(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "SESSION_NOT_FOUND", "会话不存在或不属于当前账号")
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), sessionId); err != nil {
		sessionWriteErr(c, err)
		return
	}
	SetAuditMeta(c, "session_revoke", sess.Username, "success")
	c.Status(http.StatusNoContent)
}

// ListSystemSessions GET /system/sessions
func (h *SessionHandler) ListSystemSessions(c *gin.Context, params apigen.ListSystemSessionsParams) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		claims = JWTClaims{}
	}
	userID := ""
	if params.UserId != nil {
		userID = params.UserId.String()
	}
	items, lerr := h.svc.ListAll(c.Request.Context(), userID)
	if lerr != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", lerr.Error())
		return
	}
	c.JSON(http.StatusOK, apigen.SessionList{Items: toGenSessions(items, claims.SID)})
}

// RevokeSystemSession DELETE /system/sessions/{sessionId}
func (h *SessionHandler) RevokeSystemSession(c *gin.Context, sessionId string) {
	sess, found := h.svc.Get(c.Request.Context(), sessionId)
	if !found {
		WriteProblem(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "SESSION_NOT_FOUND", "会话不存在")
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), sessionId); err != nil {
		if errors.Is(err, errSessionNotFound) {
			WriteProblem(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "SESSION_NOT_FOUND", "会话不存在")
			return
		}
		sessionWriteErr(c, err)
		return
	}
	SetAuditMeta(c, "session_revoke", sess.Username, "success")
	c.Status(http.StatusNoContent)
}

func sessionWriteErr(c *gin.Context, err error) {
	WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
}
