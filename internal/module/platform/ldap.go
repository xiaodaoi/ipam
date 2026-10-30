// Package platform LDAP/AD 目录服务（M6-007B）：设置存储与登录认证。
package platform

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrLDAPDisabled       = errors.New("LDAP_DISABLED")
	ErrLDAPUnavailable    = errors.New("LDAP_UNAVAILABLE")
	ErrLDAPUserNotFound   = errors.New("LDAP_USER_NOT_FOUND")
	ErrLDAPBadCredentials = errors.New("LDAP_BAD_CREDENTIALS")
)

// LDAPSettings 目录服务设置（单行）。
type LDAPSettings struct {
	Enabled            bool
	ServerURL          string
	StartTLS           bool
	InsecureSkipVerify bool
	BindDN             string
	BindPassword       string
	BaseDN             string
	UserFilter         string
	GroupBaseDN        string
	GroupFilter        string
	GroupAttr          string
	RoleMap            map[string]string
	DefaultRole        string
	UpdatedAt          time.Time
}

func DefaultLDAPSettings() LDAPSettings {
	return LDAPSettings{
		ServerURL:   "ldap://127.0.0.1:389",
		UserFilter:  "(uid={username})",
		GroupFilter: "(&(objectClass=groupOfNames)(member={dn}))",
		GroupAttr:   "cn",
		RoleMap:     map[string]string{},
		DefaultRole: "user",
	}
}

// LDAPRepo 设置持久化。
type LDAPRepo interface {
	Get(ctx context.Context) (LDAPSettings, error)
	Save(ctx context.Context, s LDAPSettings) (LDAPSettings, error)
}

type MemLDAPRepo struct{ s LDAPSettings }

func NewMemLDAPRepo() *MemLDAPRepo { return &MemLDAPRepo{s: DefaultLDAPSettings()} }

func (m *MemLDAPRepo) Get(context.Context) (LDAPSettings, error) { return m.s, nil }
func (m *MemLDAPRepo) Save(_ context.Context, s LDAPSettings) (LDAPSettings, error) {
	if s.RoleMap == nil {
		s.RoleMap = map[string]string{}
	}
	s.UpdatedAt = time.Now().UTC()
	m.s = s
	return s, nil
}

type PgLDAPRepo struct{ pool *pgxpool.Pool }

func NewPgLDAPRepo(pool *pgxpool.Pool) *PgLDAPRepo { return &PgLDAPRepo{pool: pool} }

func (p *PgLDAPRepo) Get(ctx context.Context) (LDAPSettings, error) {
	var s LDAPSettings
	err := p.pool.QueryRow(ctx, `
		SELECT enabled, server_url, start_tls, insecure_skip_verify, bind_dn, bind_password,
		       base_dn, user_filter, group_base_dn, group_filter, group_attr, role_map, default_role, updated_at
		  FROM ldap_settings WHERE id=1`).
		Scan(&s.Enabled, &s.ServerURL, &s.StartTLS, &s.InsecureSkipVerify, &s.BindDN, &s.BindPassword,
			&s.BaseDN, &s.UserFilter, &s.GroupBaseDN, &s.GroupFilter, &s.GroupAttr,
			&s.RoleMap, &s.DefaultRole, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultLDAPSettings(), nil
	}
	if s.RoleMap == nil {
		s.RoleMap = map[string]string{}
	}
	return s, err
}

func (p *PgLDAPRepo) Save(ctx context.Context, s LDAPSettings) (LDAPSettings, error) {
	if s.RoleMap == nil {
		s.RoleMap = map[string]string{}
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO ldap_settings(id, enabled, server_url, start_tls, insecure_skip_verify, bind_dn, bind_password,
		  base_dn, user_filter, group_base_dn, group_filter, group_attr, role_map, default_role, updated_at)
		VALUES(1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now())
		ON CONFLICT (id) DO UPDATE SET enabled=EXCLUDED.enabled, server_url=EXCLUDED.server_url,
		  start_tls=EXCLUDED.start_tls, insecure_skip_verify=EXCLUDED.insecure_skip_verify,
		  bind_dn=EXCLUDED.bind_dn, bind_password=EXCLUDED.bind_password, base_dn=EXCLUDED.base_dn,
		  user_filter=EXCLUDED.user_filter, group_base_dn=EXCLUDED.group_base_dn,
		  group_filter=EXCLUDED.group_filter, group_attr=EXCLUDED.group_attr,
		  role_map=EXCLUDED.role_map, default_role=EXCLUDED.default_role, updated_at=now()
		RETURNING enabled, server_url, start_tls, insecure_skip_verify, bind_dn, bind_password,
		  base_dn, user_filter, group_base_dn, group_filter, group_attr, role_map, default_role, updated_at`,
		s.Enabled, s.ServerURL, s.StartTLS, s.InsecureSkipVerify, s.BindDN, s.BindPassword,
		s.BaseDN, s.UserFilter, s.GroupBaseDN, s.GroupFilter, s.GroupAttr, s.RoleMap, s.DefaultRole).
		Scan(&s.Enabled, &s.ServerURL, &s.StartTLS, &s.InsecureSkipVerify, &s.BindDN, &s.BindPassword,
			&s.BaseDN, &s.UserFilter, &s.GroupBaseDN, &s.GroupFilter, &s.GroupAttr, &s.RoleMap, &s.DefaultRole, &s.UpdatedAt)
	return s, err
}

// LDAPUser 认证结果（用于 shadow user 同步）。
type LDAPUser struct {
	Username    string
	DisplayName string
	Email       string
	Groups      []string
	Roles       []string
}

// LDAPAuthenticator 目录认证（每次登录实时 bind，不缓存口令）。
type LDAPAuthenticator struct{ repo LDAPRepo }

func NewLDAPAuthenticator(repo LDAPRepo) *LDAPAuthenticator { return &LDAPAuthenticator{repo: repo} }

const ldapTimeout = 5 * time.Second

// Authenticate 服务账号搜索用户 DN → 校验用户口令 → 读取组 → 映射角色。
func (a *LDAPAuthenticator) Authenticate(ctx context.Context, username, password string) (LDAPUser, error) {
	st, err := a.repo.Get(ctx)
	if err != nil {
		return LDAPUser{}, err
	}
	if !st.Enabled {
		return LDAPUser{}, ErrLDAPDisabled
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return LDAPUser{}, ErrLDAPBadCredentials
	}
	conn, err := dialLDAP(st)
	if err != nil {
		return LDAPUser{}, fmt.Errorf("%w: %v", ErrLDAPUnavailable, err)
	}
	defer func() { _ = conn.Close() }()
	conn.SetTimeout(ldapTimeout)

	bindPw := st.BindPassword
	if v := strings.TrimSpace(os.Getenv("IPAM_LDAP_BIND_PASSWORD")); v != "" {
		bindPw = v
	}
	if st.BindDN != "" {
		if err := conn.Bind(st.BindDN, bindPw); err != nil {
			return LDAPUser{}, fmt.Errorf("%w: service bind: %v", ErrLDAPUnavailable, err)
		}
	}

	filter := strings.ReplaceAll(st.UserFilter, "{username}", ldap.EscapeFilter(username))
	if !strings.Contains(st.UserFilter, "{username}") {
		filter = fmt.Sprintf("(%s=%s)", st.UserFilter, ldap.EscapeFilter(username))
	}
	attrs := []string{"dn", "cn", "displayName", "mail", "memberOf"}
	res, err := conn.Search(ldap.NewSearchRequest(
		st.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, int(ldapTimeout.Seconds()), false,
		filter, attrs, nil))
	if err != nil {
		return LDAPUser{}, fmt.Errorf("%w: search: %v", ErrLDAPUnavailable, err)
	}
	if len(res.Entries) == 0 {
		return LDAPUser{}, ErrLDAPUserNotFound
	}
	entry := res.Entries[0]

	groups := append([]string{}, entry.GetAttributeValues("memberOf")...)
	if st.GroupBaseDN != "" {
		gf := strings.ReplaceAll(st.GroupFilter, "{dn}", ldap.EscapeFilter(entry.DN))
		if !strings.Contains(st.GroupFilter, "{dn}") {
			gf = fmt.Sprintf("(member=%s)", ldap.EscapeFilter(entry.DN))
		}
		if gres, gerr := conn.Search(ldap.NewSearchRequest(
			st.GroupBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 200, int(ldapTimeout.Seconds()), false,
			gf, []string{"dn", st.GroupAttr}, nil)); gerr == nil {
			for _, g := range gres.Entries {
				if v := g.GetAttributeValue(st.GroupAttr); v != "" {
					groups = append(groups, v)
				}
				groups = append(groups, g.DN)
			}
		}
	}

	// 用户口令校验（在服务账号搜索之后，使用用户 DN 重新 bind）。
	if err := conn.Bind(entry.DN, password); err != nil {
		return LDAPUser{}, ErrLDAPBadCredentials
	}

	display := strings.TrimSpace(entry.GetAttributeValue("displayName"))
	if display == "" {
		display = strings.TrimSpace(entry.GetAttributeValue("cn"))
	}
	if display == "" {
		display = username
	}
	roles := mapRoles(groups, st.RoleMap, st.DefaultRole)
	return LDAPUser{
		Username: username, DisplayName: display, Email: entry.GetAttributeValue("mail"),
		Groups: groups, Roles: roles,
	}, nil
}

func dialLDAP(st LDAPSettings) (*ldap.Conn, error) {
	raw := strings.TrimSpace(st.ServerURL)
	if raw == "" {
		return nil, errors.New("server_url 为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "ldap://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: st.InsecureSkipVerify, ServerName: u.Hostname()}
	opts := []ldap.DialOpt{ldap.DialWithDialer(&net.Dialer{Timeout: ldapTimeout})}
	if u.Scheme == "ldaps" || st.StartTLS {
		opts = append(opts, ldap.DialWithTLSConfig(tlsCfg))
	}
	conn, err := ldap.DialURL(raw, opts...)
	if err != nil {
		return nil, err
	}
	if st.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

// ldapGroupMatches 组标识匹配：支持组 DN 全等、组 CN/OU 值、以及不带属性的组名。
// 例："cn=ipam-ops,ou=groups,dc=corp" 可匹配 key "ipam-ops"、"cn=ipam-ops" 或完整 DN。
func ldapGroupMatches(group, key string) bool {
	gl := strings.ToLower(strings.TrimSpace(group))
	kl := strings.ToLower(strings.TrimSpace(key))
	if kl == "" {
		return false
	}
	if gl == kl || strings.HasSuffix(gl, ","+kl) {
		return true
	}
	for _, rdn := range strings.Split(gl, ",") {
		rdn = strings.TrimSpace(rdn)
		if rdn == kl {
			return true
		}
		if i := strings.Index(rdn, "="); i >= 0 && strings.TrimSpace(rdn[i+1:]) == kl {
			return true
		}
	}
	return false
}

// mapRoles 组 → 角色：支持组 DN、CN、逗号/分号分隔的角色值；无命中用 default。
func mapRoles(groups []string, roleMap map[string]string, defaultRole string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		for _, r := range strings.FieldsFunc(v, func(c rune) bool { return c == ',' || c == ';' }) {
			r = strings.TrimSpace(r)
			if r != "" && !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	for _, g := range groups {
		gl := strings.ToLower(strings.TrimSpace(g))
		if gl == "" {
			continue
		}
		for key, role := range roleMap {
			if ldapGroupMatches(gl, key) {
				add(role)
			}
		}
	}
	if len(out) == 0 && defaultRole != "" {
		add(defaultRole)
	}
	if len(out) == 0 {
		out = []string{"user"}
	}
	return out
}
