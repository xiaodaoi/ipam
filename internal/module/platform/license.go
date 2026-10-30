package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// LicenseEnvelope 离线授权文件：payload/signature 均为 base64url。
type LicenseEnvelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type LicensePayload struct {
	LicenseID    string     `json:"licenseId"`
	IssuedTo     string     `json:"issuedTo"`
	Edition      string     `json:"edition"`
	Features     []string   `json:"features"`
	MaxTerminals int        `json:"maxTerminals"`
	IssuedAt     time.Time  `json:"issuedAt"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	GraceDays    int        `json:"graceDays,omitempty"`
}

type LicenseState struct {
	Licensed      bool
	Edition       string
	LicenseID     string
	IssuedTo      string
	Features      []string
	MaxTerminals  int
	IssuedAt      time.Time
	ExpiresAt     *time.Time
	DaysRemaining int
	Message       string
	FilePath      string
}

type LicenseService struct {
	filePath string
	pub      ed25519.PublicKey
	now      func() time.Time
}

// NewLicenseService 读取 IPAM_LICENSE_FILE / IPAM_LICENSE_PUBKEY。
// 未配置公钥时保持 community，仅允许查看状态（无法激活）。
func NewLicenseService(filePath, pubKey string) *LicenseService {
	if filePath == "" {
		filePath = "/run/ipam/license.json"
	}
	return &LicenseService{filePath: filePath, pub: parseLicensePublicKey(pubKey), now: time.Now}
}

func parseLicensePublicKey(raw string) ed25519.PublicKey {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if block, _ := pem.Decode([]byte(raw)); block != nil {
		if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
			if ed, ok := key.(ed25519.PublicKey); ok {
				return ed
			}
		}
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b)
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b)
	}
	return nil
}

func (s *LicenseService) Status(context.Context) LicenseState {
	out := LicenseState{
		Edition: "community", Features: []string{}, MaxTerminals: 0,
		DaysRemaining: -1, FilePath: s.filePath,
	}
	if s.pub == nil {
		out.Message = "未配置授权公钥（IPAM_LICENSE_PUBKEY），当前为 community"
		return out
	}
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Message = "未激活 License，当前为 community"
			return out
		}
		out.Message = "读取授权文件失败: " + err.Error()
		return out
	}
	p, err := s.verify(string(raw))
	if err != nil {
		out.Message = "License 无效: " + err.Error()
		return out
	}
	out.Licensed = true
	out.Edition = p.Edition
	out.LicenseID = p.LicenseID
	out.IssuedTo = p.IssuedTo
	out.Features = p.Features
	out.MaxTerminals = p.MaxTerminals
	out.IssuedAt = p.IssuedAt
	out.ExpiresAt = p.ExpiresAt
	now := s.now().UTC()
	if p.ExpiresAt != nil {
		out.DaysRemaining = int(p.ExpiresAt.Sub(now).Hours() / 24)
	}
	out.Message = "License 有效"
	if p.ExpiresAt != nil && p.ExpiresAt.Before(now) {
		out.Licensed = false
		out.Message = "License 已过期"
	}
	return out
}

func (s *LicenseService) verify(raw string) (LicensePayload, error) {
	if s.pub == nil {
		return LicensePayload{}, errors.New("未配置授权公钥")
	}
	var env LicenseEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return LicensePayload{}, fmt.Errorf("JSON 格式错误: %w", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(env.Payload)
	if err != nil {
		if payload, err = base64.StdEncoding.DecodeString(env.Payload); err != nil {
			return LicensePayload{}, errors.New("payload 编码错误")
		}
	}
	sig, err := base64.RawURLEncoding.DecodeString(env.Signature)
	if err != nil {
		if sig, err = base64.StdEncoding.DecodeString(env.Signature); err != nil {
			return LicensePayload{}, errors.New("signature 编码错误")
		}
	}
	if !ed25519.Verify(s.pub, payload, sig) {
		return LicensePayload{}, errors.New("签名校验失败")
	}
	var p LicensePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return LicensePayload{}, errors.New("payload 内容错误")
	}
	if p.Edition != "lite" && p.Edition != "standard" && p.Edition != "enterprise" {
		return LicensePayload{}, errors.New("edition 非法")
	}
	now := s.now().UTC()
	if p.ExpiresAt != nil {
		deadline := p.ExpiresAt.Add(time.Duration(p.GraceDays) * 24 * time.Hour)
		if now.After(deadline) {
			return LicensePayload{}, errors.New("已过期")
		}
	}
	return p, nil
}

func (s *LicenseService) Activate(_ context.Context, raw string) (LicenseState, error) {
	if s.pub == nil {
		return LicenseState{}, errors.New("未配置授权公钥 IPAM_LICENSE_PUBKEY")
	}
	if _, err := s.verify(strings.TrimSpace(raw)); err != nil {
		return LicenseState{}, err
	}
	if dir := filepath.Dir(s.filePath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return LicenseState{}, err
		}
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(raw)), 0o600); err != nil {
		return LicenseState{}, err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return LicenseState{}, err
	}
	return s.Status(context.Background()), nil
}

func (s *LicenseService) Remove(context.Context) error {
	err := os.Remove(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// LicenseHandler GET/PUT/DELETE /system/license。
type LicenseHandler struct{ svc *LicenseService }

func NewLicenseHandler(svc *LicenseService) *LicenseHandler { return &LicenseHandler{svc: svc} }

func toGenLicense(s LicenseState) apigen.LicenseStatus {
	edition := apigen.LicenseStatusEdition(s.Edition)
	if edition == "" {
		edition = apigen.Community
	}
	maxTerminals := s.MaxTerminals
	days := s.DaysRemaining
	out := apigen.LicenseStatus{
		Licensed: s.Licensed, Edition: edition, Features: s.Features,
		MaxTerminals: &maxTerminals, DaysRemaining: &days, Message: s.Message,
	}
	if s.LicenseID != "" {
		out.LicenseId = &s.LicenseID
	}
	if s.IssuedTo != "" {
		out.IssuedTo = &s.IssuedTo
	}
	if !s.IssuedAt.IsZero() {
		out.IssuedAt = &s.IssuedAt
	}
	out.ExpiresAt = s.ExpiresAt
	if s.DaysRemaining != 0 {
		out.DaysRemaining = &s.DaysRemaining
	}
	out.FilePath = &s.FilePath
	return out
}

func (h *LicenseHandler) GetLicenseStatus(c *gin.Context) {
	c.JSON(http.StatusOK, toGenLicense(h.svc.Status(c.Request.Context())))
}

func (h *LicenseHandler) ActivateLicense(c *gin.Context) {
	var body apigen.LicenseActivateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		WriteProblem(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	st, err := h.svc.Activate(c.Request.Context(), body.License)
	if err != nil {
		WriteProblem(c, http.StatusBadRequest, "https://ipam.local/problems/license", "LICENSE_INVALID", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenLicense(st))
}

func (h *LicenseHandler) RemoveLicense(c *gin.Context) {
	if err := h.svc.Remove(c.Request.Context()); err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
