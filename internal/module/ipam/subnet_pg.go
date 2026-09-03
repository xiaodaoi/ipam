package ipam

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/netip"
)

// PgSubnetRepo PG 实现（迁移 0003）。
type PgSubnetRepo struct{ pool *pgxpool.Pool }

func NewSubnetRepo(pool *pgxpool.Pool) *PgSubnetRepo { return &PgSubnetRepo{pool: pool} }

// nullInt *int → pgx 可空参数（nil=NULL）。
func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// pdRangeEnd PD 池范围尾推导（prefix+prefix-len 的最后一个地址，M2-018）。
func pdRangeEnd(prefix string, plen int) (string, error) {
	ip, err := netip.ParseAddr(prefix)
	if err != nil {
		return "", err
	}
	a16 := ip.As16()
	hostBits := 128 - plen
	for i := 0; i < hostBits; i++ {
		a16[15-i/8] |= 1 << (uint(i) % 8)
	}
	return netip.AddrFrom16(a16).String(), nil
}

const subnetCols = `id, coalesce(org_id::text,''), name, family, cidr::text,
  coalesce(kea_subnet_id,0), coalesce(description,''), coalesce(gateway,''), coalesce(dns_servers,'')`

func scanSubnet(row pgx.Row) (Subnet, error) {
	var s Subnet
	err := row.Scan(&s.ID, &s.OrgID, &s.Name, &s.Family, &s.CIDR, &s.KeaSubnetID, &s.Description,
		&s.Gateway, &s.DNSServers)
	return s, err
}

// loadPools 按 subnet_id 分组回填地址池。
// 修复前 List/Get 只读 subnet 主表：Pools 恒空 → 台账逐池枚举 0 行、kea 下发空池（M2-017 诊断发现）。
func (r *PgSubnetRepo) loadPools(ctx context.Context, subs []Subnet) error {
	if len(subs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(subs))
	for i := range subs {
		ids = append(ids, subs[i].ID)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT subnet_id::text, host(start_addr), host(end_addr), kind, prefix_len, delegated_len
		 FROM address_pool WHERE subnet_id::text = ANY($1) ORDER BY start_addr`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	bySub := map[string][]Pool{}
	for rows.Next() {
		var sid, start, end, kind string
		var plen, dlen *int
		if err := rows.Scan(&sid, &start, &end, &kind, &plen, &dlen); err != nil {
			return err
		}
		bySub[sid] = append(bySub[sid], Pool{StartAddr: start, EndAddr: end, Kind: kind, PrefixLen: plen, DelegatedLen: dlen})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range subs {
		subs[i].Pools = bySub[subs[i].ID]
	}
	return nil
}

func (r *PgSubnetRepo) List(ctx context.Context, orgID string, family int) ([]Subnet, error) {
	q := `SELECT ` + subnetCols + ` FROM subnet WHERE true`
	var args []any
	if orgID != "" {
		q += ` AND org_id = $` + fmt1(len(args)+1)
		args = append(args, orgID)
	}
	if family != 0 {
		q += ` AND family = $` + fmt1(len(args)+1)
		args = append(args, family)
	}
	q += ` ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subnet
	for rows.Next() {
		s, err := scanSubnet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := r.loadPools(ctx, out); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func (r *PgSubnetRepo) Get(ctx context.Context, id string) (Subnet, bool, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+subnetCols+` FROM subnet WHERE id=$1`, id)
	s, err := scanSubnet(row)
	if err == pgx.ErrNoRows {
		return Subnet{}, false, nil
	}
	if err != nil {
		return Subnet{}, false, err
	}
	return s, true, nil
}

func (r *PgSubnetRepo) Create(ctx context.Context, s Subnet) (Subnet, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO subnet(org_id,name,family,cidr,kea_subnet_id,description,gateway,dns_servers)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		nullStr(s.OrgID), s.Name, s.Family, s.CIDR, s.KeaSubnetID, nullStr(s.Description), s.Gateway, s.DNSServers).Scan(&id)
	if err != nil {
		return Subnet{}, err
	}
	s.ID = id
	if err := insertPools(ctx, r, id, s); err != nil {
		return Subnet{}, err
	}
	return s, nil
}

// insertPools 池落库（pd 池 endAddr 由 prefix+prefix-len 推导，M2-018）。
func insertPools(ctx context.Context, r *PgSubnetRepo, id string, s Subnet) error {
	for i := range s.Pools {
		p := &s.Pools[i]
		endAddr := p.EndAddr
		plen, dlen := nullInt(p.PrefixLen), nullInt(p.DelegatedLen)
		if p.Kind == "pd" {
			if p.PrefixLen == nil || p.DelegatedLen == nil {
				return errors.New("PD pool requires prefixLen/delegatedLen")
			}
			end, err := pdRangeEnd(p.StartAddr, *p.PrefixLen)
			if err != nil {
				return err
			}
			endAddr = end
			p.EndAddr = end // 回填推导值（创建响应/内存投影一致）
		}
		if _, err := r.pool.Exec(ctx,
			`INSERT INTO address_pool(subnet_id,family,start_addr,end_addr,kind,prefix_len,delegated_len)
			 VALUES($1,$2,$3,$4,$5,$6,$7)`,
			id, s.Family, p.StartAddr, endAddr, p.Kind, plen, dlen); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgSubnetRepo) Update(ctx context.Context, s Subnet) (Subnet, error) {
	if _, err := r.pool.Exec(ctx,
		`UPDATE subnet SET name=$2, cidr=$3, gateway=$4, dns_servers=$5, description=$6, updated_at=now() WHERE id=$1`,
		s.ID, s.Name, s.CIDR, s.Gateway, s.DNSServers, nullStr(s.Description)); err != nil {
		return Subnet{}, err
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM address_pool WHERE subnet_id=$1`, s.ID); err != nil {
		return Subnet{}, err
	}
	if err := insertPools(ctx, r, s.ID, s); err != nil {
		return Subnet{}, err
	}
	return s, nil
}

func (r *PgSubnetRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM subnet WHERE id=$1`, id)
	return err
}

// OrgRepo PG 实现（迁移 0001）。
type OrgRepo struct{ pool *pgxpool.Pool }

func NewOrgRepo(pool *pgxpool.Pool) *OrgRepo { return &OrgRepo{pool: pool} }

func (r *OrgRepo) List() []OrgNode {
	rows, err := r.pool.Query(context.Background(),
		`SELECT id::text, coalesce(parent_id::text,''), name, path FROM org_group`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []OrgNode
	for rows.Next() {
		var n OrgNode
		if err := rows.Scan(&n.ID, &n.ParentID, &n.Name, &n.Path); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func (r *OrgRepo) Get(id string) (OrgNode, bool) {
	var n OrgNode
	err := r.pool.QueryRow(context.Background(),
		`SELECT id::text, coalesce(parent_id::text,''), name, path FROM org_group WHERE id=$1`, id).
		Scan(&n.ID, &n.ParentID, &n.Name, &n.Path)
	if err != nil {
		return OrgNode{}, false
	}
	return n, true
}

func (r *OrgRepo) Create(n OrgNode) error {
	_, err := r.pool.Exec(context.Background(),
		`INSERT INTO org_group(id,parent_id,name,path) VALUES($1,$2,$3,$4)`,
		n.ID, nullStr(n.ParentID), n.Name, n.Path)
	return err
}

func (r *OrgRepo) Update(n OrgNode) {
	_, _ = r.pool.Exec(context.Background(),
		`UPDATE org_group SET parent_id=$2, name=$3, path=$4 WHERE id=$1`,
		n.ID, nullStr(n.ParentID), n.Name, n.Path)
}

func (r *OrgRepo) Delete(id string) error {
	_, err := r.pool.Exec(context.Background(), `DELETE FROM org_group WHERE id=$1`, id)
	return err
}

func (r *OrgRepo) HasChildren(id string) bool {
	var one int
	err := r.pool.QueryRow(context.Background(),
		`SELECT 1 FROM org_group WHERE parent_id=$1 LIMIT 1`, id).Scan(&one)
	return err == nil
}

func (r *OrgRepo) ReferencedByAsset(id string) bool {
	var one int
	err := r.pool.QueryRow(context.Background(),
		`SELECT 1 FROM asset WHERE org_id=$1 LIMIT 1`, id).Scan(&one)
	return err == nil
}

// MemReservationRepo 内存实现（PoC/单测）。
type MemReservationRepo struct{ items map[string]Reservation }

func NewMemReservationRepo() *MemReservationRepo {
	return &MemReservationRepo{items: map[string]Reservation{}}
}

func (r *MemReservationRepo) Upsert(_ context.Context, res Reservation) error {
	r.items[res.IPv4] = res
	return nil
}

func (r *MemReservationRepo) List(_ context.Context) ([]Reservation, error) {
	out := []Reservation{}
	for _, v := range r.items {
		out = append(out, v)
	}
	return out, nil
}

func (r *MemReservationRepo) Delete(_ context.Context, ipv4 string) error {
	delete(r.items, ipv4)
	return nil
}

func (r *MemReservationRepo) UpdateMAC(_ context.Context, ipv4, mac string) error {
	if _, ok := r.items[ipv4]; !ok {
		return ErrAddrNotReserved
	}
	r.items[ipv4] = Reservation{MAC: mac, IPv4: ipv4}
	return nil
}

// PgReservationRepo PG 实现。
type PgReservationRepo struct{ pool *pgxpool.Pool }

func NewPgReservationRepo(pool *pgxpool.Pool) *PgReservationRepo {
	return &PgReservationRepo{pool: pool}
}

func (r *PgReservationRepo) Upsert(ctx context.Context, res Reservation) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO reservation(mac, ipv4, origin) VALUES($1,$2,'manual')
		 ON CONFLICT (ipv4) WHERE ipv4 IS NOT NULL
		 DO UPDATE SET mac = EXCLUDED.mac`,
		nullStr(res.MAC), res.IPv4)
	return err
}

func (r *PgReservationRepo) Delete(ctx context.Context, ipv4 string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM reservation WHERE ipv4=$1`, ipv4)
	return err
}

func (r *PgReservationRepo) UpdateMAC(ctx context.Context, ipv4, mac string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE reservation SET mac=$1 WHERE ipv4=$2`, nullStr(mac), ipv4)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAddrNotReserved
	}
	return nil
}

func (r *PgReservationRepo) List(ctx context.Context) ([]Reservation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT coalesce(mac,''), host(ipv4) FROM reservation WHERE ipv4 IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reservation{}
	for rows.Next() {
		var res Reservation
		if err := rows.Scan(&res.MAC, &res.IPv4); err == nil {
			out = append(out, res)
		}
	}
	return out, rows.Err()
}

// MemSubnetRepo 内存实现（PoC/单测）。
type MemSubnetRepo struct {
	mu    sync.RWMutex
	items []Subnet
}

func NewMemSubnetRepo() *MemSubnetRepo { return &MemSubnetRepo{} }

func (r *MemSubnetRepo) List(_ context.Context, orgID string, family int) ([]Subnet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Subnet{}
	for _, s := range r.items {
		if orgID != "" && s.OrgID != orgID {
			continue
		}
		if family != 0 && s.Family != family {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
func (r *MemSubnetRepo) Get(_ context.Context, id string) (Subnet, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.items {
		if s.ID == id {
			return s, true, nil
		}
	}
	return Subnet{}, false, nil
}
func (r *MemSubnetRepo) Create(_ context.Context, s Subnet) (Subnet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s.ID = uuid.NewString()
	r.items = append(r.items, s)
	return s, nil
}
func (r *MemSubnetRepo) Update(_ context.Context, s Subnet) (Subnet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.items {
		if r.items[i].ID == s.ID {
			s.OrgID, s.Family, s.KeaSubnetID = r.items[i].OrgID, r.items[i].Family, r.items[i].KeaSubnetID
			r.items[i] = s
			return s, nil
		}
	}
	return Subnet{}, ErrSubnetNotFound
}
func (r *MemSubnetRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.items {
		if r.items[i].ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			return nil
		}
	}
	return ErrSubnetNotFound
}

// NoopKea 无引擎环境占位（dryRun 语义：返回 1）。
type NoopKea struct{}

func NewNoopKea() *NoopKea { return &NoopKea{} }

func (n *NoopKea) DeploySubnet(_ context.Context, _ []Subnet, _ bool) (int, error) { return 1, nil }
func (n *NoopKea) RemoveSubnet(_ context.Context, _ int) error                     { return nil }

// LoadLedgerBindings 台账绑定源：读 PG coherence_binding（active/grace）。
func LoadLedgerBindings(ctx context.Context, pool *pgxpool.Pool) ([]LedgerBinding, error) {
	rows, err := pool.Query(ctx,
		`SELECT coalesce(mac,''), host(ipv4), coalesce(host(ipv6),''), coalesce(hostname,''), state
		 FROM coherence_binding WHERE state IN ('active','grace')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerBinding{}
	for rows.Next() {
		var b LedgerBinding
		if err := rows.Scan(&b.MAC, &b.IPv4, &b.IPv6, &b.Hostname, &b.State); err == nil {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// MemAssetRepo 内存实现（PoC/单测）。
type MemAssetRepo struct{ items map[string]Asset }

func NewMemAssetRepo() *MemAssetRepo { return &MemAssetRepo{items: map[string]Asset{}} }

func (r *MemAssetRepo) List(_ context.Context, orgID, q string) ([]Asset, error) {
	out := []Asset{}
	for _, a := range r.items {
		if orgID != "" && a.OrgID != orgID {
			continue
		}
		if q != "" && !strings.Contains(a.MAC, q) && !strings.Contains(a.Owner, q) && !strings.Contains(a.Dept, q) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
func (r *MemAssetRepo) Upsert(_ context.Context, a Asset) error {
	r.items[a.MAC] = a
	return nil
}
func (r *MemAssetRepo) Delete(_ context.Context, mac string) error {
	delete(r.items, mac)
	return nil
}

// PgAssetRepo PG 实现。
type PgAssetRepo struct{ pool *pgxpool.Pool }

func NewPgAssetRepo(pool *pgxpool.Pool) *PgAssetRepo { return &PgAssetRepo{pool: pool} }

func (r *PgAssetRepo) List(ctx context.Context, orgID, q string) ([]Asset, error) {
	sql := `SELECT mac, coalesce(org_id::text,''), coalesce(owner,''), coalesce(dept,''), coalesce(note,''), coalesce(tags,'{}') FROM asset WHERE true`
	var args []any
	if orgID != "" {
		args = append(args, orgID)
		sql += ` AND org_id = $` + fmt1(len(args))
	}
	if q != "" {
		args = append(args, "%"+q+"%")
		sql += ` AND (mac ILIKE $` + fmt1(len(args)) + ` OR owner ILIKE $` + fmt1(len(args)) + ` OR dept ILIKE $` + fmt1(len(args)) + `)`
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Asset{}
	for rows.Next() {
		var a Asset
		var tags []string
		if err := rows.Scan(&a.MAC, &a.OrgID, &a.Owner, &a.Dept, &a.Note, &tags); err == nil {
			a.Tags = tags
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

func (r *PgAssetRepo) Upsert(ctx context.Context, a Asset) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO asset(mac, org_id, owner, dept, note, tags, updated_at)
		 VALUES($1,$2,$3,$4,$5,$6,now())
		 ON CONFLICT (mac) DO UPDATE SET org_id=EXCLUDED.org_id, owner=EXCLUDED.owner,
		   dept=EXCLUDED.dept, note=EXCLUDED.note, tags=EXCLUDED.tags, updated_at=now()`,
		a.MAC, nullStr(a.OrgID), a.Owner, nullStr(a.Dept), nullStr(a.Note), a.Tags)
	return err
}

func (r *PgAssetRepo) Delete(ctx context.Context, mac string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM asset WHERE mac=$1`, mac)
	return err
}
