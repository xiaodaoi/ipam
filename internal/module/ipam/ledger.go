package ipam

import (
	"errors"
	"net/netip"
	"sort"
	"time"
)

// LedgerState 地址六态（§13.4 颜色规范）。
type LedgerState string

const (
	StateOnline    LedgerState = "online"
	StateAvailable LedgerState = "available"
	StateReserved  LedgerState = "reserved"
	StateStatic    LedgerState = "static"
	StateGrace     LedgerState = "grace"
	StateConflict  LedgerState = "conflict"
)

// LedgerRow 台账行（v4 逐地址 / v6 子网级汇总）。
type LedgerRow struct {
	Address     string
	Family      int
	State       LedgerState
	MAC         string
	Hostname    string
	Owner       string
	LeaseExpiry time.Time
	SubnetID    string
	PoolIndex   string
}

// LedgerQuery 过滤条件。
type LedgerQuery struct {
	OrgID    string
	SubnetID string
	Family   int
	State    LedgerState
	Cursor   string
	PageSize int
}

// LedgerBinding 绑定投影（来自 coherence 域；隔离耦合）。
type LedgerBinding struct {
	MAC      string
	IPv4     string
	IPv6     string
	Hostname string
	State    string
}

// LedgerSource 台账数据源聚合（绑定/预留/资产/子网池）。
type LedgerSource struct {
	Bindings     []LedgerBinding
	Reservations []Reservation
	Assets       map[string]Asset // mac -> asset（asset.go 完整类型）
	Subnets      []Subnet
}

// Reservation 预留投影（PG reservation 表）。
type Reservation struct {
	MAC  string
	IPv4 string
}

// ErrAddrOccupied 地址已被绑定/在线占用。
var ErrAddrOccupied = errors.New("ADDR_OCCUPIED")

// ErrAddrNotReserved 地址不存在预留/绑定记录（释放/改绑目标缺失）。
var ErrAddrNotReserved = errors.New("ADDR_NOT_RESERVED")

// ErrBadMAC MAC 格式非法（规范化后非完整 48 位）。
var ErrBadMAC = errors.New("BAD_MAC")

// Classify 六态判定（§13.4）：租约活跃近似=在线（绿）。
func Classify(r Reservation, b *LedgerBinding, inPool bool) LedgerState {
	switch {
	case r.MAC != "" && r.IPv4 != "":
		return StateStatic // 静态绑定（蓝）
	case r.IPv4 != "" && r.MAC == "":
		return StateReserved // 保留冻结（黄）
	case b != nil:
		switch b.State {
		case "grace":
			return StateGrace // 过期宽限（橙）
		case "conflict":
			return StateConflict // 冲突（红）
		default:
			return StateOnline // 租约活跃近似（绿）
		}
	case inPool:
		return StateAvailable // 空闲（灰蓝）
	default:
		return StateAvailable
	}
}

// Query 组装台账页：v4 逐地址（含池内枚举）排序 + 游标切片。
func QueryLedger(src LedgerSource, q LedgerQuery) ([]LedgerRow, string, int) {
	rows := []LedgerRow{}

	// v4：逐地址行
	for _, s := range src.Subnets {
		if q.Family != 0 && s.Family != q.Family {
			continue
		}
		if q.OrgID != "" && s.OrgID != q.OrgID {
			continue
		}
		if q.SubnetID != "" && s.ID != q.SubnetID {
			continue
		}
		byAddr := map[string]*LedgerBinding{}
		for i := range src.Bindings {
			byAddr[src.Bindings[i].IPv4] = &src.Bindings[i]
		}
		resByAddr := map[string]Reservation{}
		for _, r := range src.Reservations {
			resByAddr[r.IPv4] = r
		}
		if s.Family == 4 {
			for _, p := range s.Pools {
				from := netip.MustParseAddr(p.StartAddr)
				to := netip.MustParseAddr(p.EndAddr)
				cur := from
				guard := 0
				for cur.Compare(to) <= 0 && guard < 100000 {
					guard++
					addr := cur.String()
					row := LedgerRow{
						Address:   addr,
						Family:    4,
						State:     Classify(resByAddr[addr], byAddr[addr], true),
						SubnetID:  s.ID,
						PoolIndex: "4:" + addr,
					}
					if b := byAddr[addr]; b != nil {
						row.MAC = b.MAC
						row.Hostname = b.Hostname
						row.LeaseExpiry = time.Now().Add(30 * time.Minute) // 租约近似
					}
					if r := resByAddr[addr]; r.MAC != "" {
						row.MAC = r.MAC
					}
					if a := src.Assets[row.MAC]; a.MAC != "" {
						row.Owner = a.Owner
					}
					rows = append(rows, row)
					cur = cur.Next()
				}
			}
		} else {
			// v6：子网级汇总行
			rows = append(rows, LedgerRow{
				Address:   s.CIDR,
				Family:    6,
				State:     StateAvailable,
				SubnetID:  s.ID,
				PoolIndex: "6:" + s.CIDR,
			})
		}
	}

	if q.State != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if r.State == q.State {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].PoolIndex < rows[j].PoolIndex })

	total := len(rows)
	if q.Cursor != "" {
		idx := sort.Search(len(rows), func(i int) bool { return rows[i].PoolIndex > q.Cursor })
		rows = rows[idx:]
	}
	page := q.PageSize
	if page <= 0 {
		page = 100
	}
	next := ""
	if len(rows) > page {
		next = rows[page-1].PoolIndex
		rows = rows[:page]
	}
	if len(rows) == 0 {
		next = ""
	}
	return rows, next, total
}
