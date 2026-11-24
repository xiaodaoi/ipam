package ipam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func newAssetTestRouter(t *testing.T) (*gin.Engine, *AssetHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewAssetHandler(NewAssetService(NewMemAssetRepo(), NewMemOrgStore()))
	r := gin.New()
	r.PUT("/assets", h.UpsertAsset)
	r.GET("/assets", func(c *gin.Context) { h.ListAssets(c, apigen.ListAssetsParams{}) })
	r.DELETE("/assets/:mac", func(c *gin.Context) { h.DeleteAsset(c, c.Param("mac")) })
	r.GET("/assets/discovered", h.ListDiscoveredTerminals)
	return r, h
}

func doJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAssetHandler_IdentityValidationAndDelete(t *testing.T) {
	r, _ := newAssetTestRouter(t)

	// 缺身份键 → 400 BAD_IDENTITY（M6-046：MAC/DUID 至少一个）
	w := doJSON(t, r, http.MethodPut, "/assets", `{"owner":"张三"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "BAD_IDENTITY") {
		t.Fatalf("缺身份键应 400 BAD_IDENTITY，实际 %d %s", w.Code, w.Body.String())
	}

	// DUID-only 登记成功，identity 回退 DUID
	w = doJSON(t, r, http.MethodPut, "/assets", `{"duid":"000100012DF9B6D9F8A2D6C97867","owner":"李四"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("DUID-only 登记失败: %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["identity"] != "000100012df9b6d9f8a2d6c97867" || got["mac"] != nil {
		t.Fatalf("identity/mac 不符合预期: %s", w.Body.String())
	}

	// 删除不存在的身份键 → 404（此前 PG 实现不检查 RowsAffected，永远 204）
	w = doJSON(t, r, http.MethodDelete, "/assets/aa:bb:cc:dd:ee:99", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("删除不存在的资产应 404，实际 %d", w.Code)
	}

	// 列表带 identity
	w = doJSON(t, r, http.MethodGet, "/assets", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"identity"`) {
		t.Fatalf("列表缺少 identity: %d %s", w.Code, w.Body.String())
	}
}

func TestAssetHandler_DiscoveredKeepsIPv6Identity(t *testing.T) {
	r, h := newAssetTestRouter(t)
	h.SetBindingSource(func(context.Context) ([]LedgerBinding, error) {
		return []LedgerBinding{
			{MAC: "000100012f51e6c570089431ccb5", IPv4: "0.0.0.0", IPv6: "2406::1",
				State: "active", Hostname: "v6only", Cltt: 4102444800, ValidLft: 3600,
				SubnetID: "s1", SubnetCIDR: "2406::/64", OrgID: "o1"},
		}, nil
	})
	w := doJSON(t, r, http.MethodGet, "/assets/discovered", "")
	if w.Code != http.StatusOK {
		t.Fatalf("discovered 失败: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("IPv6-only 终端应出现在发现列表: %s", w.Body.String())
	}
	it := out.Items[0]
	if it["identity"] != "70:08:94:31:cc:b5" || it["ipv6"] != "2406::1" ||
		it["subnetCidr"] != "2406::/64" || it["orgId"] != "o1" {
		t.Fatalf("发现项字段不符合预期: %v", it)
	}
	if it["ipv4"] != nil {
		t.Fatalf("0.0.0.0 不应作为 IPv4 返回: %v", it)
	}
}
