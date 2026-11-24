package ipam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func newAssetTestRouter(t *testing.T) (*gin.Engine, *AssetHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	svc.SetIgnoredRepo(NewMemIgnoredRepo())
	h := NewAssetHandler(svc)
	r := gin.New()
	r.PUT("/assets", h.UpsertAsset)
	r.GET("/assets", func(c *gin.Context) {
		params := apigen.ListAssetsParams{}
		if v := c.Query("online"); v != "" {
			b := v == "true"
			params.Online = &b
		}
		h.ListAssets(c, params)
	})
	r.DELETE("/assets/:mac", func(c *gin.Context) { h.DeleteAsset(c, c.Param("mac")) })
	r.POST("/assets/bulk", h.BulkUpsertAssets)
	r.GET("/assets/ignored", h.ListIgnoredTerminals)
	r.PUT("/assets/ignored", h.IgnoreTerminal)
	r.DELETE("/assets/ignored/:identity", func(c *gin.Context) { h.DeleteIgnoredTerminal(c, c.Param("identity")) })
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

func TestAssetHandler_OnlineEnrichmentAndFilter(t *testing.T) {
	r, h := newAssetTestRouter(t)
	h.SetBindingSource(func(context.Context) ([]LedgerBinding, error) {
		now := time.Now()
		return []LedgerBinding{
			{MAC: "AA:BB:CC:DD:EE:01", IPv4: "10.0.0.10", State: "active",
				Cltt: now.Add(-time.Minute).Unix(), ValidLft: 3600},
		}, nil
	})
	if w := doJSON(t, r, http.MethodPut, "/assets", `{"mac":"aa:bb:cc:dd:ee:01","owner":"张三"}`); w.Code != http.StatusOK {
		t.Fatalf("登记失败: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, r, http.MethodPut, "/assets", `{"mac":"aa:bb:cc:dd:ee:02","owner":"李四"}`); w.Code != http.StatusOK {
		t.Fatalf("登记失败: %d %s", w.Code, w.Body.String())
	}

	var out struct {
		Items []struct {
			Identity string `json:"identity"`
			IPv4     string `json:"ipv4"`
			Online   bool   `json:"online"`
			State    string `json:"state"`
		} `json:"items"`
		OnlineCount int `json:"onlineCount"`
		Total       int `json:"total"`
	}
	w := doJSON(t, r, http.MethodGet, "/assets", "")
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || out.OnlineCount != 1 || out.Total != 2 {
		t.Fatalf("列表应 2 条/1 条在线: %s", w.Body.String())
	}
	for _, it := range out.Items {
		if it.Identity == "aa:bb:cc:dd:ee:01" && (!it.Online || it.IPv4 != "10.0.0.10" || it.State != "active") {
			t.Fatalf("在线资产未带租约视图: %+v", it)
		}
		if it.Identity == "aa:bb:cc:dd:ee:02" && it.Online {
			t.Fatalf("无租约资产不应在线: %+v", it)
		}
	}

	// online=false 只返回离线资产
	w = doJSON(t, r, http.MethodGet, "/assets?online=false", "")
	out.Items, out.OnlineCount = nil, 0
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Items) != 1 || out.Items[0].Identity != "aa:bb:cc:dd:ee:02" {
		t.Fatalf("online=false 过滤异常: %s", w.Body.String())
	}
}

func TestAssetHandler_BulkAndIgnore(t *testing.T) {
	r, h := newAssetTestRouter(t)

	w := doJSON(t, r, http.MethodPost, "/assets/bulk",
		`{"items":[{"mac":"aa:bb:cc:dd:ee:01","owner":"张三"},{"duid":"000100012df9b6d9f8a2d6c97867","owner":"李四"},{"mac":"bad","owner":"王五"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("批量登记失败: %d %s", w.Code, w.Body.String())
	}
	var bulk struct {
		OK       bool `json:"ok"`
		Saved    int  `json:"saved"`
		Failures []struct {
			Index int    `json:"index"`
			Code  string `json:"code"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bulk); err != nil {
		t.Fatal(err)
	}
	if bulk.OK || bulk.Saved != 2 || len(bulk.Failures) != 1 ||
		bulk.Failures[0].Index != 2 || bulk.Failures[0].Code != "BAD_MAC" {
		t.Fatalf("批量部分成功语义不符: %s", w.Body.String())
	}

	// 空 items → 400
	if w = doJSON(t, r, http.MethodPost, "/assets/bulk", `{"items":[]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("空 items 应 400，实际 %d", w.Code)
	}

	// 忽略名单：加入 → 列表 → 恢复 → 404
	if w = doJSON(t, r, http.MethodPut, "/assets/ignored", `{"mac":"aa:bb:cc:dd:ee:01","reason":"打印机"}`); w.Code != http.StatusOK {
		t.Fatalf("忽略失败: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet, "/assets/ignored", "")
	if !strings.Contains(w.Body.String(), `"reason":"打印机"`) {
		t.Fatalf("忽略名单缺少原因: %s", w.Body.String())
	}
	// 忽略后 Discover 不再返回该终端（租约来自注入的绑定源）
	h.SetBindingSource(func(context.Context) ([]LedgerBinding, error) {
		now := time.Now()
		return []LedgerBinding{
			{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.0.0.10", State: "active",
				Cltt: now.Add(-time.Minute).Unix(), ValidLft: 3600},
			{MAC: "aa:bb:cc:dd:ee:09", IPv4: "10.0.0.19", State: "active",
				Cltt: now.Add(-time.Minute).Unix(), ValidLft: 3600},
		}, nil
	})
	w = doJSON(t, r, http.MethodGet, "/assets/discovered", "")
	var disc struct {
		Items []struct {
			Identity string `json:"identity"`
		} `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &disc)
	if len(disc.Items) != 1 || disc.Items[0].Identity != "aa:bb:cc:dd:ee:09" {
		t.Fatalf("忽略终端仍出现在发现列表: %s", w.Body.String())
	}
	if w = doJSON(t, r, http.MethodDelete, "/assets/ignored/aa:bb:cc:dd:ee:01", ""); w.Code != http.StatusNoContent {
		t.Fatalf("恢复失败: %d", w.Code)
	}
	if w = doJSON(t, r, http.MethodDelete, "/assets/ignored/aa:bb:cc:dd:ee:01", ""); w.Code != http.StatusNotFound {
		t.Fatalf("重复恢复应 404，实际 %d", w.Code)
	}
}
