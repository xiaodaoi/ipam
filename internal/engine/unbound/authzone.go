package unbound

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/xiaodaoi/ipam/internal/pkg/rpz"
)

// runOut 执行 unbound-control 并返回标准输出（用于 list_auth_zones 等查询命令）。
func (e ExecController) runOut(args ...string) (string, error) {
	bin := e.Bin
	if bin == "" {
		bin = "unbound-control"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", ErrUnavailable
	}
	out, err := exec.Command(bin, e.cmdArgs(args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %v: %v", bin, args, err)
	}
	return string(out), nil
}

// AuthZoneSerials 查询运行态 auth-zone/RPZ 区的 serial（M6-038 实际态校正）。
// 只返回正常加载（serial 可解析）的区；退化/未加载的区不出现，调用方据此重下发。
func (e ExecController) AuthZoneSerials(_ context.Context) (map[string]uint32, error) {
	out, err := e.runOut("list_auth_zones")
	if err != nil {
		return nil, err
	}
	return rpz.SerialMap(rpz.ParseAuthZones(out)), nil
}

// AuthZoneTransfer 触发单区从 master 拉取（P1 外部情报 AXFR/IXFR 用）。
func (e ExecController) AuthZoneTransfer(_ context.Context, zone string) error {
	if strings.TrimSpace(zone) == "" {
		return ErrUnavailable
	}
	return e.run("auth_zone_transfer", zone)
}

// FastReload 保持缓存的快速重载（M6-038：需要整机刷新时优先于 reload）。
func (e ExecController) FastReload(_ context.Context) error {
	return e.run("fast_reload")
}

// RPZEnable 运行态启用某 RPZ 区（排障/灰度用）。
func (e ExecController) RPZEnable(_ context.Context, zone string) error {
	if strings.TrimSpace(zone) == "" {
		return ErrUnavailable
	}
	return e.run("rpz_enable", zone)
}

// RPZDisable 运行态停用某 RPZ 区（排障/灰度用；旧数据不再匹配）。
func (e ExecController) RPZDisable(_ context.Context, zone string) error {
	if strings.TrimSpace(zone) == "" {
		return ErrUnavailable
	}
	return e.run("rpz_disable", zone)
}
