// Package lease Kea 租约模型（kea 与 dhcp 模块共享，避免循环依赖）。
package lease

// Lease4 Kea DHCPv4 租约投影（lease_cmds hook，memfile 不入库）。
type Lease4 struct {
	IPAddress     string `json:"ip-address"`
	HWAddress     string `json:"hw-address,omitempty"`
	ClientID      string `json:"client-id,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	Cltt          int64  `json:"cltt,omitempty"`
	ValidLifetime uint32 `json:"valid-lft,omitempty"`
	State         int    `json:"state"`
	SubnetID      uint32 `json:"subnet-id,omitempty"`
}
