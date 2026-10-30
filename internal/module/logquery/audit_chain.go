package logquery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// auditChainHash 计算一条审计的 SHA256 链哈希。
// 参与字段：上一条哈希 + 自增 ID + 时间 + 身份 + 方法/路径/动作/资源 + 状态 + detail + 脱敏请求体。
func auditChainHash(e AuditEntry, prev string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%d\n%s\n%s",
		prev, e.ID, e.TS.UTC().Format(time.RFC3339Nano),
		e.ActorType, e.Actor, e.TokenSub, e.Method, e.Path, e.Action,
		e.Resource, e.Status, e.Detail, e.Payload)
	return hex.EncodeToString(h.Sum(nil))
}
