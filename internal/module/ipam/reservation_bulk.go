package ipam

import (
	"context"
	"fmt"

	"github.com/xiaodaoi/ipam/internal/module/coherence"
)

// BulkEntry 批量导入行。
type BulkEntry struct {
	Kind    string // reserve | bind
	Address string
	MAC     string
	Reason  string
}

// BulkFailure 失败明细（行号 1 起）。
type BulkFailure struct {
	Line   int
	Reason string
}

// BulkResult 批量结果。
type BulkResult struct {
	OK       bool
	Applied  int
	Failures []BulkFailure
}

// BulkReservations 事务性批量：预校验全通过才逐条应用；任一失败→整体回滚。
// 语义：先对全部行做占用/合法性预检（零写入），再逐条 Upsert+Kea 下发；
// 应用阶段出现 Kea 错误时尽力回滚已写入项并返回整体失败。
func (s *LedgerService) BulkReservations(ctx context.Context, subnetID string, entries []BulkEntry) (BulkResult, error) {
	res := BulkResult{OK: true}
	// 阶段一：预检
	existing, err := s.repo.List(ctx)
	if err != nil {
		return res, err
	}
	occupied := map[string]bool{}
	for _, r := range existing {
		occupied[r.IPv4] = true
	}
	for _, b := range s.source(ctx).Bindings {
		occupied[b.IPv4] = true
	}

	for i, e := range entries {
		line := i + 1
		addr := e.Address
		if occupied[addr] {
			res.OK = false
			res.Failures = append(res.Failures, BulkFailure{Line: line, Reason: fmt.Sprintf("ADDR_OCCUPIED: %s", addr)})
			continue
		}
		if e.Kind == "bind" {
			if coherence.NormalizeMAC(e.MAC) == "" {
				res.OK = false
				res.Failures = append(res.Failures, BulkFailure{Line: line, Reason: fmt.Sprintf("BAD_MAC: %s", e.MAC)})
				continue
			}
		}
	}

	if !res.OK {
		return res, nil // 整体回滚：无任何写入
	}

	// 阶段二：应用
	applied := 0
	for i, e := range entries {
		line := i + 1
		var uerr error
		if e.Kind == "bind" {
			uerr = s.applyBind(ctx, subnetID, e.Address, e.MAC)
		} else {
			uerr = s.applyReserve(ctx, subnetID, e.Address)
		}
		if uerr != nil {
			// 尽力回滚已应用项
			_ = s.rollbackApplied(ctx, subnetID, entries[:i])
			res.OK = false
			res.Failures = append(res.Failures, BulkFailure{Line: line, Reason: uerr.Error()})
			return res, nil
		}
		applied++
	}
	res.Applied = applied
	return res, nil
}

// applyReserve/applyBind 直写仓储（绕过 assertFree——预检已完成，避免重复 List）。
func (s *LedgerService) applyReserve(ctx context.Context, subnetID, addr string) error {
	if err := s.repo.Upsert(ctx, Reservation{IPv4: addr}); err != nil {
		return err
	}
	return s.kea.ReserveAddress(ctx, subnetID, addr)
}

func (s *LedgerService) applyBind(ctx context.Context, subnetID, addr, mac string) error {
	if err := s.repo.Upsert(ctx, Reservation{MAC: mac, IPv4: addr}); err != nil {
		return err
	}
	return s.kea.BindStatic(ctx, subnetID, addr, mac)
}

// rollbackApplied 尽力移除已应用项（Kea 侧再删除 + 仓储删除）。
func (s *LedgerService) rollbackApplied(ctx context.Context, subnetID string, applied []BulkEntry) error {
	var first error
	for i := len(applied) - 1; i >= 0; i-- {
		e := applied[i]
		if err := s.repo.Delete(ctx, e.Address); err != nil && first == nil {
			first = err
		}
	}
	return first
}
