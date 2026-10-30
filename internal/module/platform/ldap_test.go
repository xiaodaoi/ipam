package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// fakeDirectory 目录认证桩（M6-007B 单测：不依赖真实 LDAP）。
type fakeDirectory struct {
	user     LDAPUser
	password string
	err      error
}

func (f *fakeDirectory) Authenticate(_ context.Context, _, password string) (LDAPUser, error) {
	if f.err != nil {
		return LDAPUser{}, f.err
	}
	if password == "" || (f.password != "" && password != f.password) {
		return LDAPUser{}, ErrLDAPBadCredentials
	}
	return f.user, nil
}

func ldapTestRouter(t *testing.T) (*gin.Engine, *MemUserStore, *MemLDAPRepo, *fakeDirectory) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	repo := NewMemLDAPRepo()
	dir := &fakeDirectory{}
	authH := NewAuthHandler(store, NewTokenBlacklist(), nil)
	authH.SetLDAP(dir)
	ldapH := NewLDAPHandler(repo, nil)

	r := gin.New()
	r.POST("/api/v1/auth/login", authH.AuthLogin)
	r.POST("/api/v1/auth/password", authH.ChangeAuthPassword)
	r.GET("/api/v1/system/ldap", ldapH.GetLDAPSettings)
	r.PUT("/api/v1/system/ldap", ldapH.UpdateLDAPSettings)
	return r, store, repo, dir
}

func TestMapRoles(t *testing.T) {
	cases := []struct {
		name    string
		groups  []string
		roleMap map[string]string
		def     string
		want    []string
	}{
		{"组CN命中", []string{"cn=ipam-ops,ou=groups,dc=corp"}, map[string]string{"ipam-ops": "operator"}, "user", []string{"operator"}},
		{"组DN命中", []string{"cn=ops,ou=g,dc=corp"}, map[string]string{"cn=ops,ou=g,dc=corp": "admin"}, "user", []string{"admin"}},
		{"多角色合并去重", []string{"cn=a,dc=x", "cn=b,dc=x"}, map[string]string{"cn=a": "admin,auditor", "cn=b": "auditor"}, "user", []string{"admin", "auditor"}},
		{"未命中用默认", []string{"cn=other,dc=x"}, map[string]string{"cn=a": "admin"}, "auditor", []string{"auditor"}},
		{"空组且无默认", nil, nil, "", []string{"user"}},
	}
	for _, c := range cases {
		got := mapRoles(c.groups, c.roleMap, c.def)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestLDAPHandler_SaveAndMaskPassword(t *testing.T) {
	r, _, repo, _ := ldapTestRouter(t)
	body := `{"enabled":true,"serverUrl":"ldap://ldap.corp:389","baseDn":"dc=corp,dc=local",
		"userFilter":"(uid={username})","bindDn":"cn=svc,dc=corp,dc=local","bindPassword":"Secret!23",
		"roleMap":{"ipam-ops":"operator"},"defaultRole":"user"}`
	w := doJSON(r, http.MethodPut, "/api/v1/system/ldap", "", body)
	if w.Code != http.StatusOK {
		t.Fatalf("保存失败 %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Secret!23") {
		t.Fatal("响应不应回显 bindPassword")
	}
	var got apigen.LDAPSettings
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.HasBindPassword == nil || !*got.HasBindPassword {
		t.Fatal("应标记 hasBindPassword=true")
	}
	if st, _ := repo.Get(context.Background()); st.BindPassword != "Secret!23" {
		t.Fatalf("口令未落库: %+v", st)
	}

	// 留空表白口令保持不变（避免前端读取后回写空值清空）
	body2 := `{"enabled":true,"serverUrl":"ldap://ldap.corp:389","baseDn":"dc=corp,dc=local",
		"userFilter":"(uid={username})","bindPassword":"","roleMap":{},"defaultRole":"user"}`
	if w := doJSON(r, http.MethodPut, "/api/v1/system/ldap", "", body2); w.Code != http.StatusOK {
		t.Fatalf("二次保存失败 %d: %s", w.Code, w.Body.String())
	}
	if st, _ := repo.Get(context.Background()); st.BindPassword != "Secret!23" {
		t.Fatal("空 bindPassword 不应清空原口令")
	}
}

func TestLDAPHandler_EnableRequiresBaseDN(t *testing.T) {
	r, _, _, _ := ldapTestRouter(t)
	w := doJSON(r, http.MethodPut, "/api/v1/system/ldap", "",
		`{"enabled":true,"serverUrl":"ldap://ldap.corp:389","baseDn":"","userFilter":"(uid={username})","roleMap":{},"defaultRole":"user"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("启用但缺 baseDn 应 400，实际 %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_LDAPCreatesShadowAndMapsRoles(t *testing.T) {
	r, store, repo, dir := ldapTestRouter(t)
	if _, err := repo.Save(context.Background(), LDAPSettings{Enabled: true, ServerURL: "ldap://x", BaseDN: "dc=x"}); err != nil {
		t.Fatal(err)
	}
	dir.user = LDAPUser{Username: "zhangsan", DisplayName: "张三", Roles: []string{"operator"}}
	dir.password = "dir-pass"

	w, res := loginWith(t, r, "zhangsan", "dir-pass")
	if w.Code != http.StatusOK || res.AccessToken == "" {
		t.Fatalf("目录登录失败 %d: %s", w.Code, w.Body.String())
	}
	rec, ok, _ := store.GetByUsername(context.Background(), "zhangsan")
	if !ok || rec.AuthSource != "ldap" {
		t.Fatalf("应创建 ldap shadow 账号: %+v", rec)
	}
	if strings.Join(rec.Roles, ",") != "operator" {
		t.Fatalf("角色应来自目录映射: %v", rec.Roles)
	}
	// shadow 账号本地口令为随机值，不能用空口令登录
	if _, res2 := loginWith(t, r, "zhangsan", ""); res2.AccessToken != "" {
		t.Fatal("空口令不应通过")
	}

	// 目录角色变化 → 下次登录同步并吊销旧会话（token_version+1）
	dir.user = LDAPUser{Username: "zhangsan", DisplayName: "张三", Roles: []string{"admin"}}
	if w, _ := loginWith(t, r, "zhangsan", "dir-pass"); w.Code != http.StatusOK {
		t.Fatalf("二次目录登录失败 %d", w.Code)
	}
	rec2, _, _ := store.GetByUsername(context.Background(), "zhangsan")
	if strings.Join(rec2.Roles, ",") != "admin" || rec2.TokenVersion <= rec.TokenVersion {
		t.Fatalf("角色同步/会话吊销未生效: roles=%v ver=%d", rec2.Roles, rec2.TokenVersion)
	}
}

func TestLogin_LocalUnchangedWhenDirectoryDown(t *testing.T) {
	r, store, _, dir := ldapTestRouter(t)
	dir.err = ErrLDAPUnavailable
	// 本地 admin 不受目录故障影响
	if w, res := loginWith(t, r, "admin", "admin123"); w.Code != http.StatusOK || res.AccessToken == "" {
		t.Fatalf("目录故障时本地账号应可登录: %d %s", w.Code, w.Body.String())
	}
	// 目录侧已有绑定用户 → 目录不可达返回 503
	if _, err := store.Create(context.Background(), UserCreateInput{
		Username: "ldapuser", DisplayName: "L", Roles: []string{"user"}, AuthSource: "ldap",
		Password: "x",
	}); err != nil {
		t.Fatal(err)
	}
	if w, _ := loginWith(t, r, "ldapuser", "whatever"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("目录不可达应 503，实际 %d", w.Code)
	}
}

func TestLogin_LDAPDisabledForUnknownUser(t *testing.T) {
	r, _, _, dir := ldapTestRouter(t)
	dir.err = ErrLDAPDisabled
	// 未启用目录服务 → 未知账号按口令错误处理，不泄露认证后端状态
	w, _ := loginWith(t, r, "ghost", "x")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("应 401，实际 %d: %s", w.Code, w.Body.String())
	}
}

func TestDirectoryAccountCannotChangeLocalPassword(t *testing.T) {
	r, store, repo, _ := ldapTestRouter(t)
	if _, err := repo.Save(context.Background(), LDAPSettings{Enabled: true, ServerURL: "ldap://x", BaseDN: "dc=x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), UserCreateInput{
		Username: "dir1", DisplayName: "D", Roles: []string{"user"}, AuthSource: "ldap", Password: "x",
	}); err != nil {
		t.Fatal(err)
	}

	// 直接签发令牌模拟目录登录成功后改密
	rec, _, _ := store.GetByUsername(context.Background(), "dir1")
	tok := IssueTokenFor(rec.Username, rec.ID, rec.Roles, rec.TokenVersion)
	w := doJSON(r, http.MethodPost, "/api/v1/auth/password", tok,
		`{"oldPassword":"x","newPassword":"NewPassw0rd!23"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "DIRECTORY_ACCOUNT") {
		t.Fatalf("目录账号本地改密应 400 DIRECTORY_ACCOUNT，实际 %d: %s", w.Code, w.Body.String())
	}
}
