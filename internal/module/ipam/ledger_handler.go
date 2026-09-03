package ipam

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/module/coherence"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// ReservationRepo 预留持久化（PG reservation 表 / 内存实现）。
type ReservationRepo interface {
	Upsert(ctx context.Context, r Reservation) error
	List(ctx context.Context) ([]Reservation, error)
	Delete(ctx context.Context, ipv4 string) error
	// UpdateMAC 更新既有预留记录的 MAC（保留↔静态绑定互转语义）。记录不存在返回 ErrAddrNotReserved。
	UpdateMAC(ctx context.Context, ipv4, mac string) error
}

// LedgerService 台账查询与操作。
type LedgerService struct {
	source func(ctx context.Context) LedgerSource
	repo   ReservationRepo
	subs   SubnetRepo
	apply  func(ctx context.Context) error
}

func NewLedgerService(source func(context.Context) LedgerSource, repo ReservationRepo, subs SubnetRepo, apply func(context.Context) error) *LedgerService {
	return &LedgerService{source: source, repo: repo, subs: subs, apply: apply}
}

// notifyApply 统一下发触发（M3-007 配置式：整段 config-set；apply 未注入则跳过——PoC 内存模式）。
func (s *LedgerService) notifyApply(ctx context.Context) error {
	if s.apply == nil {
		return nil
	}
	return s.apply(ctx)
}

// Query 见 QueryLedger。
func (s *LedgerService) Query(ctx context.Context, q LedgerQuery) ([]LedgerRow, string, int) {
	return QueryLedger(s.source(ctx), q)
}

// Reserve 转保留：占用检查→写预留→Kea 下发（下发失败回滚预留，保持 DB↔Kea 一致）。
func (s *LedgerService) Reserve(ctx context.Context, subnetID, addr string) error {
	if err := s.assertFree(ctx, addr); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, Reservation{IPv4: addr}); err != nil {
		return err
	}
	if err := s.notifyApply(ctx); err != nil {
		_ = s.repo.Delete(ctx, addr)
		return err
	}
	return nil
}

// BindStatic 转静态绑定：占用检查→写 MAC 预留→Kea host reservation（下发失败回滚）。
func (s *LedgerService) BindStatic(ctx context.Context, subnetID, addr, mac string) error {
	if len(coherence.NormalizeMAC(mac)) != 17 {
		return ErrBadMAC
	}
	if err := s.assertFree(ctx, addr); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, Reservation{MAC: mac, IPv4: addr}); err != nil {
		return err
	}
	if err := s.notifyApply(ctx); err != nil {
		_ = s.repo.Delete(ctx, addr)
		return err
	}
	return nil
}

// assertFree 地址已被绑定或在线占用则拒绝（409 ADDR_OCCUPIED）。
// 占用判定以持久化预留仓储为准（source 仅提供在线绑定热数据）。
func (s *LedgerService) assertFree(ctx context.Context, addr string) error {
	src := s.source(ctx)
	for _, b := range src.Bindings {
		if b.IPv4 == addr {
			return ErrAddrOccupied
		}
	}
	existing, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if r.IPv4 == addr {
			return ErrAddrOccupied
		}
	}
	return nil
}

// findReserved 定位地址的预留记录；不存在返回 ErrAddrNotReserved。
func (s *LedgerService) findReserved(ctx context.Context, addr string) (*Reservation, error) {
	existing, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range existing {
		if existing[i].IPv4 == addr {
			return &existing[i], nil
		}
	}
	return nil, ErrAddrNotReserved
}

// Release 释放保留/静态绑定地址：删除预留记录→Kea 重新下发后回归动态池。
// 在线租约不受影响（到期/续租由 DHCP 自然收敛）。下发失败回滚删除。
func (s *LedgerService) Release(ctx context.Context, addr string) error {
	prev, err := s.findReserved(ctx, addr)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, addr); err != nil {
		return err
	}
	if err := s.notifyApply(ctx); err != nil {
		_ = s.repo.Upsert(ctx, *prev)
		return err
	}
	return nil
}

// UpdateBinding 修改既有预留的 MAC；保留地址写入 MAC 即成为静态绑定。下发失败回滚。
func (s *LedgerService) UpdateBinding(ctx context.Context, addr, mac string) error {
	norm := coherence.NormalizeMAC(mac)
	if len(norm) != 17 {
		return ErrBadMAC
	}
	prev, err := s.findReserved(ctx, addr)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateMAC(ctx, addr, norm); err != nil {
		return err
	}
	if err := s.notifyApply(ctx); err != nil {
		_ = s.repo.UpdateMAC(ctx, addr, prev.MAC)
		return err
	}
	return nil
}

// LedgerHandler 实现 apigen.ServerInterface 中 ledger 域端点（§13.4 地址台账）。
type LedgerHandler struct {
	svc *LedgerService
}

func NewLedgerHandler(svc *LedgerService) *LedgerHandler { return &LedgerHandler{svc: svc} }

// ListLedger GET /ledger
func (h *LedgerHandler) ListLedger(c *gin.Context, params apigen.ListLedgerParams) {
	orgID := ""
	if params.OrgId != nil {
		orgID = params.OrgId.String()
	}
	family := 0
	if params.Family != nil {
		family = int(*params.Family)
	}
	state := ""
	if params.State != nil {
		state = string(*params.State)
	}
	cursor := ""
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	pageSize := 100
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	subnetID := ""
	if params.SubnetId != nil {
		subnetID = params.SubnetId.String()
	}
	rows, next, total := h.svc.Query(c.Request.Context(), LedgerQuery{
		OrgID: orgID, SubnetID: subnetID, Family: family, State: LedgerState(state), Cursor: cursor, PageSize: pageSize,
	})
	items := make([]apigen.LedgerRow, 0, len(rows))
	for _, r := range rows {
		items = append(items, toGenLedger(r))
	}
	var nextPtr *string
	if next != "" {
		nextPtr = &next
	}
	c.JSON(http.StatusOK, apigen.LedgerPage{Items: items, NextCursor: nextPtr, Total: &total})
}

// ReserveAddress POST /ledger
func (h *LedgerHandler) ReserveAddress(c *gin.Context) {
	var body apigen.ReserveRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if err := h.svc.Reserve(c.Request.Context(), body.SubnetId.String(), body.Address); err != nil {
		occupied(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// BindStatic POST /ledger/bind
func (h *LedgerHandler) BindStatic(c *gin.Context) {
	var body apigen.BindRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if err := h.svc.BindStatic(c.Request.Context(), body.SubnetId.String(), body.Address, body.Mac); err != nil {
		occupied(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateBinding PUT /ledger/bind
func (h *LedgerHandler) UpdateBinding(c *gin.Context) {
	var body apigen.UpdateBindingRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if err := h.svc.UpdateBinding(c.Request.Context(), body.Address, body.Mac); err != nil {
		notReserved(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ReleaseAddress POST /ledger/release
func (h *LedgerHandler) ReleaseAddress(c *gin.Context) {
	var body apigen.ReleaseRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if err := h.svc.Release(c.Request.Context(), body.Address); err != nil {
		notReserved(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func occupied(c *gin.Context, err error) {
	if errors.Is(err, ErrAddrOccupied) {
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/addr-occupied", "ADDR_OCCUPIED", "地址已被绑定或在线占用")
		return
	}
	problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
}

func notReserved(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrAddrNotReserved):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/addr-not-reserved", "ADDR_NOT_RESERVED", "地址不存在预留/绑定记录")
	case errors.Is(err, ErrBadMAC):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-mac", "BAD_MAC", "MAC 格式非法")
	default:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
	}
}

func toGenLedger(r LedgerRow) apigen.LedgerRow {
	row := apigen.LedgerRow{
		Address:   r.Address,
		Family:    apigen.LedgerRowFamily(r.Family),
		State:     apigen.LedgerState(r.State),
		PoolIndex: strPtr(r.PoolIndex),
		SubnetId:  uuidPtr(r.SubnetID),
	}
	if r.MAC != "" {
		row.Mac = strPtr(r.MAC)
	}
	if r.Hostname != "" {
		row.Hostname = strPtr(r.Hostname)
	}
	if r.Owner != "" {
		row.Owner = strPtr(r.Owner)
	}
	if !r.LeaseExpiry.IsZero() {
		row.LeaseExpiry = &r.LeaseExpiry
	}
	return row
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uuidPtr(s string) *rtypes.UUID {
	if s == "" {
		return nil
	}
	u := guuid.MustParse(s)
	return (*rtypes.UUID)(&u)
}

// BulkReservations POST /reservations/bulk
func (h *LedgerHandler) BulkReservations(c *gin.Context) {
	var body apigen.ReservationBulkRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	entries := make([]BulkEntry, 0, len(body.Entries))
	for _, e := range body.Entries {
		entries = append(entries, BulkEntry{
			Kind:    string(e.Kind),
			Address: e.Address,
			MAC:     derefStr(e.Mac),
			Reason:  derefStr(e.Reason),
		})
	}
	res, err := h.svc.BulkReservations(c.Request.Context(), body.SubnetId.String(), entries)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	failures := make([]struct {
		Line   int    `json:"line"`
		Reason string `json:"reason"`
	}, 0, len(res.Failures))
	for _, f := range res.Failures {
		failures = append(failures, struct {
			Line   int    `json:"line"`
			Reason string `json:"reason"`
		}{Line: f.Line, Reason: f.Reason})
	}
	c.JSON(http.StatusOK, apigen.ReservationBulkResult{Ok: res.OK, Applied: res.Applied, Failures: failures})
}
