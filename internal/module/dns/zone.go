package dns

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Zone 本地区域（PG dns_zone 行）。
type Zone struct {
	ID      string
	Name    string
	Kind    string // auth | local
	Enabled bool
}

// Record 解析记录（PG dns_record 行）。
type Record struct {
	ID      string
	ZoneID  string
	Name    string
	RecType string
	TTL     int
	Rdata   string
	Enabled bool
}

// ZoneRepo 持久化（zone + record）。
type ZoneRepo interface {
	ListZones(ctx context.Context) ([]Zone, error)
	CreateZone(ctx context.Context, z Zone) (Zone, error)
	UpdateZone(ctx context.Context, z Zone) error
	DeleteZone(ctx context.Context, id string) error
	ListRecords(ctx context.Context, zoneID string) ([]Record, error)
	CreateRecord(ctx context.Context, r Record) (Record, error)
	UpdateRecord(ctx context.Context, r Record) error
	DeleteRecord(ctx context.Context, id string) error
}

var (
	ErrZoneNotFound   = errors.New("ZONE_NOT_FOUND")
	ErrRecordNotFound = errors.New("RECORD_NOT_FOUND")
	ErrRecordNameDup  = errors.New("RECORD_NAME_DUP")
	ErrZoneNameDup    = errors.New("ZONE_NAME_DUP")
	ErrBadRdata       = errors.New("BAD_RDATA")
)

// ValidateRecord 按类型校验 rdata 语法（§13.4 正向/反向记录）。
func ValidateRecord(recType, rdata string) error {
	switch recType {
	case "A":
		ip := net.ParseIP(rdata)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("%w: %s 非合法 A", ErrBadRdata, rdata)
		}
	case "AAAA":
		ip := net.ParseIP(rdata)
		if ip == nil || ip.To16() == nil || ip.To4() != nil {
			return fmt.Errorf("%w: %s 非合法 AAAA", ErrBadRdata, rdata)
		}
	case "CNAME", "PTR":
		if !strings.HasSuffix(rdata, ".") {
			return fmt.Errorf("%w: %s 须为 FQDN（尾点）", ErrBadRdata, rdata)
		}
	default:
		return fmt.Errorf("%w: 未知类型 %s", ErrBadRdata, recType)
	}
	return nil
}

// ZoneService 记录 CRUD + 变更触发全量渲染/reload。
type ZoneService struct {
	repo ZoneRepo
	ctl  UnboundController
	// ApplyConf 全量渲染+reload 钩子（main.go 装配为 confApplier.apply）。
	// local-zone/local-data 渲染进文件，运行时变更须重渲染+reload 才生效与持久（M3-011）。
	ApplyConf func(ctx context.Context) error
}

func NewZoneService(repo ZoneRepo, ctl UnboundController) *ZoneService {
	return &ZoneService{repo: repo, ctl: ctl}
}

// apply 触发全量重渲染+reload（最佳努力：失败仅日志，一致性由下次收敛兜底）。
func (s *ZoneService) apply(ctx context.Context) {
	if s.ApplyConf == nil {
		return
	}
	if err := s.ApplyConf(ctx); err != nil {
		log.Printf("[zone] apply failed: %v", err)
	}
}

// CreateZone 创建区域。
func (s *ZoneService) CreateZone(ctx context.Context, z Zone) (Zone, error) {
	if !strings.HasSuffix(z.Name, ".") {
		z.Name += "."
	}
	saved, err := s.repo.CreateZone(ctx, z)
	if err != nil {
		return Zone{}, err
	}
	s.apply(ctx)
	return saved, nil
}

// UpdateZone 更新区域（重命名/启停/类型）。
func (s *ZoneService) UpdateZone(ctx context.Context, z Zone) (Zone, error) {
	if !strings.HasSuffix(z.Name, ".") {
		z.Name += "."
	}
	if err := s.repo.UpdateZone(ctx, z); err != nil {
		return Zone{}, err
	}
	s.apply(ctx)
	return z, nil
}

// DeleteZone 删除区域（级联记录）。
func (s *ZoneService) DeleteZone(ctx context.Context, id string) error {
	if err := s.repo.DeleteZone(ctx, id); err != nil {
		return err
	}
	s.apply(ctx)
	return nil
}

// CreateRecord 校验→落库→全量重渲染/reload。
func (s *ZoneService) CreateRecord(ctx context.Context, zoneID string, r Record) (Record, error) {
	if err := ValidateRecord(r.RecType, r.Rdata); err != nil {
		return Record{}, err
	}
	r.ZoneID = zoneID
	saved, err := s.repo.CreateRecord(ctx, r)
	if err != nil {
		return Record{}, err
	}
	s.apply(ctx)
	return saved, nil
}

// UpdateRecord 更新记录（校验→落库→重渲染）。
func (s *ZoneService) UpdateRecord(ctx context.Context, r Record) (Record, error) {
	if err := ValidateRecord(r.RecType, r.Rdata); err != nil {
		return Record{}, err
	}
	if err := s.repo.UpdateRecord(ctx, r); err != nil {
		return Record{}, err
	}
	s.apply(ctx)
	return r, nil
}

// DeleteRecord 删除→重渲染。
func (s *ZoneService) DeleteRecord(ctx context.Context, id string) error {
	if err := s.repo.DeleteRecord(ctx, id); err != nil {
		return err
	}
	s.apply(ctx)
	return nil
}

// ExportZonefile 组装 zonefile 文本（$ORIGIN + 记录行；auth_zone_reload 数据源）。
func ExportZonefile(zone Zone, records []Record) string {
	var sb strings.Builder
	origin := strings.TrimSuffix(zone.Name, ".")
	sb.WriteString("$ORIGIN " + zone.Name + "\n")
	sb.WriteString("$TTL 300\n")
	for _, r := range records {
		if !r.Enabled {
			continue
		}
		name := r.Name
		if !strings.HasSuffix(name, ".") {
			name += "." + zone.Name
		}
		ttl := r.TTL
		if ttl <= 0 {
			ttl = 300
		}
		fmt.Fprintf(&sb, "%s %d IN %s %s\n", name, ttl, r.RecType, r.Rdata)
	}
	_ = origin
	return sb.String()
}

// MemZoneRepo 内存实现。
type MemZoneRepo struct {
	mu      sync.Mutex
	zones   map[string]Zone
	records map[string]Record
	zseq    int
	rseq    int
}

func NewMemZoneRepo() *MemZoneRepo {
	return &MemZoneRepo{zones: map[string]Zone{}, records: map[string]Record{}}
}

func (r *MemZoneRepo) ListZones(_ context.Context) ([]Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Zone{}
	for _, z := range r.zones {
		out = append(out, z)
	}
	return out, nil
}
func (r *MemZoneRepo) CreateZone(_ context.Context, z Zone) (Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.zones {
		if strings.EqualFold(e.Name, z.Name) {
			return Zone{}, ErrZoneNameDup
		}
	}
	r.zseq++
	z.ID = "z-" + strconv.Itoa(r.zseq)
	r.zones[z.ID] = z
	return z, nil
}
func (r *MemZoneRepo) DeleteZone(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.zones, id)
	return nil
}
func (r *MemZoneRepo) UpdateZone(_ context.Context, z Zone) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.zones[z.ID]; !ok {
		return ErrZoneNotFound
	}
	for _, e := range r.zones {
		if e.ID != z.ID && strings.EqualFold(e.Name, z.Name) {
			return ErrZoneNameDup
		}
	}
	r.zones[z.ID] = z
	return nil
}
func (r *MemZoneRepo) ListRecords(_ context.Context, zoneID string) ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Record{}
	for _, rc := range r.records {
		if rc.ZoneID == zoneID {
			out = append(out, rc)
		}
	}
	return out, nil
}
func (r *MemZoneRepo) CreateRecord(_ context.Context, rec Record) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.records {
		if e.ZoneID == rec.ZoneID && strings.EqualFold(e.Name, rec.Name) && e.RecType == rec.RecType {
			return Record{}, ErrRecordNameDup
		}
	}
	r.rseq++
	rec.ID = "r-" + strconv.Itoa(r.rseq)
	r.records[rec.ID] = rec
	return rec, nil
}
func (r *MemZoneRepo) DeleteRecord(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[id]; !ok {
		return ErrRecordNotFound
	}
	delete(r.records, id)
	return nil
}
func (r *MemZoneRepo) UpdateRecord(_ context.Context, rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[rec.ID]; !ok {
		return ErrRecordNotFound
	}
	for _, e := range r.records {
		if e.ID != rec.ID && e.ZoneID == rec.ZoneID && strings.EqualFold(e.Name, rec.Name) && e.RecType == rec.RecType {
			return ErrRecordNameDup
		}
	}
	r.records[rec.ID] = rec
	return nil
}

// PgZoneRepo PG 实现（迁移 0006）。
type PgZoneRepo struct{ pool *pgxpool.Pool }

func NewPgZoneRepo(pool *pgxpool.Pool) *PgZoneRepo { return &PgZoneRepo{pool: pool} }

func (r *PgZoneRepo) ListZones(ctx context.Context) ([]Zone, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, kind, enabled FROM dns_zone ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Zone{}
	for rows.Next() {
		var z Zone
		if err := rows.Scan(&z.ID, &z.Name, &z.Kind, &z.Enabled); err == nil {
			out = append(out, z)
		}
	}
	return out, rows.Err()
}
func (r *PgZoneRepo) CreateZone(ctx context.Context, z Zone) (Zone, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO dns_zone(name, kind, enabled) VALUES($1,$2,$3) RETURNING id::text`,
		z.Name, z.Kind, z.Enabled).Scan(&id)
	z.ID = id
	return z, err
}
func (r *PgZoneRepo) DeleteZone(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM dns_zone WHERE id=$1`, id)
	return err
}
func (r *PgZoneRepo) UpdateZone(ctx context.Context, z Zone) error {
	tag, err := r.pool.Exec(ctx, `UPDATE dns_zone SET name=$1, kind=$2, enabled=$3 WHERE id=$4`,
		z.Name, z.Kind, z.Enabled, z.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrZoneNotFound
	}
	return nil
}
func (r *PgZoneRepo) ListRecords(ctx context.Context, zoneID string) ([]Record, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, zone_id::text, name, rec_type, ttl, rdata, enabled FROM dns_record WHERE zone_id=$1`, zoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.ID, &rec.ZoneID, &rec.Name, &rec.RecType, &rec.TTL, &rec.Rdata, &rec.Enabled); err == nil {
			out = append(out, rec)
		}
	}
	return out, rows.Err()
}
func (r *PgZoneRepo) CreateRecord(ctx context.Context, rec Record) (Record, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO dns_record(zone_id, name, rec_type, ttl, rdata, enabled) VALUES($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (zone_id,name,rec_type) DO NOTHING RETURNING id::text`,
		rec.ZoneID, rec.Name, rec.RecType, rec.TTL, rec.Rdata, rec.Enabled).Scan(&id)
	if err == pgx.ErrNoRows {
		return Record{}, ErrRecordNameDup
	}
	rec.ID = id
	return rec, err
}
func (r *PgZoneRepo) DeleteRecord(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM dns_record WHERE id=$1`, id)
	return err
}
func (r *PgZoneRepo) UpdateRecord(ctx context.Context, rec Record) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE dns_record SET name=$1, rec_type=$2, ttl=$3, rdata=$4, enabled=$5 WHERE id=$6`,
		rec.Name, rec.RecType, rec.TTL, rec.Rdata, rec.Enabled, rec.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrRecordNameDup
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRecordNotFound
	}
	return nil
}
