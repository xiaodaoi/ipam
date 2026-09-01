package platform

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	rtypes "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// WebuiSettings Web 页面设置（M2-039）。
type WebuiSettings struct {
	SiteName   string
	FaviconUrl string
	LogoUrl    string
	ServerPort int
}

// WebuiRepo 设置存储抽象（单行表 webui_settings）。
type WebuiRepo interface {
	Get(ctx context.Context) (WebuiSettings, error)
	Save(ctx context.Context, v WebuiSettings) error
}

// MemWebuiRepo 内存实现（PoC/单测）。
type MemWebuiRepo struct {
	mu sync.Mutex
	s  WebuiSettings
}

func NewMemWebuiRepo() *MemWebuiRepo { return &MemWebuiRepo{} }

func (s *MemWebuiRepo) Get(_ context.Context) (WebuiSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s, nil
}

func (s *MemWebuiRepo) Save(_ context.Context, v WebuiSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s = v
	return nil
}

// PgWebuiRepo PG 实现（webui_settings 单行表，迁移 0017）。
type PgWebuiRepo struct {
	pool *pgxpool.Pool
}

func NewPgWebuiRepo(pool *pgxpool.Pool) *PgWebuiRepo { return &PgWebuiRepo{pool: pool} }

func (s *PgWebuiRepo) Get(ctx context.Context) (WebuiSettings, error) {
	var v WebuiSettings
	err := s.pool.QueryRow(ctx,
		`SELECT coalesce(site_name,''), coalesce(favicon_url,''), coalesce(logo_url,''), coalesce(server_port, 8443) FROM webui_settings WHERE id`).
		Scan(&v.SiteName, &v.FaviconUrl, &v.LogoUrl, &v.ServerPort)
	if err != nil {
		return WebuiSettings{}, err
	}
	return v, nil
}

func (s *PgWebuiRepo) Save(ctx context.Context, v WebuiSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webui_settings (id, site_name, favicon_url, logo_url, server_port) VALUES (true, $1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET site_name = EXCLUDED.site_name, favicon_url = EXCLUDED.favicon_url, logo_url = EXCLUDED.logo_url, server_port = EXCLUDED.server_port, updated_at = now()`,
		v.SiteName, v.FaviconUrl, v.LogoUrl, v.ServerPort)
	return err
}

// WebuiHandler Web 页面设置（M2-039，system 域——中间件已拦 system 读写）。
type WebuiHandler struct {
	repo WebuiRepo
}

func NewWebuiHandler(repo WebuiRepo) *WebuiHandler { return &WebuiHandler{repo: repo} }

func derefPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func hostPortOf(c *gin.Context) (ip, port string) {
	host, port, err := net.SplitHostPort(c.Request.Host)
	if err != nil {
		return c.Request.Host, ""
	}
	return host, port
}

func (h *WebuiHandler) respond(c *gin.Context, v WebuiSettings) {
	ip, port := hostPortOf(c)
	portInt := 0
	if v.ServerPort > 0 {
		portInt = v.ServerPort
	} else if p, err := strconv.Atoi(port); err == nil {
		portInt = p
	}
	c.JSON(http.StatusOK, rtypes.WebuiSettings{
		SiteName:   v.SiteName,
		FaviconUrl: &v.FaviconUrl,
		LogoUrl:    &v.LogoUrl,
		ServerIp:   &ip,
		ServerPort: &portInt,
	})
}

// GetWebuiSettings GET /system/webui-settings——serverIp/serverPort 从请求 Host 解析（只读）。
func (h *WebuiHandler) GetWebuiSettings(c *gin.Context) {
	v, err := h.repo.Get(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.respond(c, v)
}

// UpdateWebuiSettings PUT /system/webui-settings——serverIp/serverPort 为只读字段（忽略）。
func (h *WebuiHandler) UpdateWebuiSettings(c *gin.Context) {
	var body rtypes.WebuiSettingsUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if body.SiteName == nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "siteName is required")
		return
	}
	port := 8443
	if body.ServerPort != nil && *body.ServerPort >= 1 && *body.ServerPort <= 65535 {
		port = *body.ServerPort
	}
	v := WebuiSettings{SiteName: *body.SiteName, FaviconUrl: derefPtr(body.FaviconUrl), LogoUrl: derefPtr(body.LogoUrl), ServerPort: port}
	if err := h.repo.Save(c.Request.Context(), v); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.respond(c, v)
}

// RestartControlPlane POST /system/restart——优雅退出（容器 restart 策略自动拉起）。
func (h *WebuiHandler) RestartControlPlane(c *gin.Context) {
	c.Status(http.StatusNoContent)
	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Printf("control-plane restart requested via /system/restart, exiting")
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
}
