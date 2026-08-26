// Package problem 统一 RFC 9457 错误出口（§12.2 约定 4：code 供机器判读与 AI 自纠重试）。
package problem

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Write 以 application/problem+json 输出错误详情。
func Write(c *gin.Context, status int, probType, code, detail string) {
	body := gin.H{
		"type":   probType,
		"title":  http.StatusText(status),
		"status": status,
	}
	if code != "" {
		body["code"] = code
	}
	if detail != "" {
		body["detail"] = detail
	}
	if c.Request != nil && c.Request.URL != nil {
		body["instance"] = c.Request.URL.Path
	}
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, body)
}
