package coherence

import (
	"strings"
	"testing"
)

func TestDecodeKeaResult_数组形态(t *testing.T) {
	// Kea 常规响应：顶层数组
	body := `[{"result":0,"text":"ok","arguments":{"leases":[{"ip-address":"10.0.0.15","hw-address":"f8:e4:3b:e9:39:8d","state":0,"subnet-id":1}]}}]`
	parsed, err := decodeKeaResult[keaLeaseResp](strings.NewReader(body))
	if err != nil {
		t.Fatalf("数组形态不应报错: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Result != 0 {
		t.Fatalf("数组形态解析错误: %+v", parsed)
	}
	leases := parsed[0].Arguments.Leases
	if len(leases) != 1 || leases[0].IPAddress != "10.0.0.15" || leases[0].HWAddress != "f8:e4:3b:e9:39:8d" {
		t.Fatalf("租约字段解析错误: %+v", leases)
	}
}

func TestDecodeKeaResult_单对象形态(t *testing.T) {
	// result=3（未找到）时 Kea 只回单对象，此前硬失败（M5-032）
	body := `{"result":3,"text":"lease not found"}`
	parsed, err := decodeKeaResult[keaLeaseResp](strings.NewReader(body))
	if err != nil {
		t.Fatalf("单对象形态不应报错: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Result != 3 || parsed[0].Text != "lease not found" {
		t.Fatalf("单对象形态解析错误: %+v", parsed)
	}
	if n := len(parsed[0].Arguments.Leases); n != 0 {
		t.Fatalf("result=3 应无租约, got %d", n)
	}
}

func TestDecodeKeaResult_非法输入(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"空响应体", ""},
		{"纯空白", "  \n\t "},
		{"非法 JSON", `{not json`},
		{"类型不匹配", `[{"result":"zero"}]`},
	}
	for _, c := range cases {
		if _, err := decodeKeaResult[keaLeaseResp](strings.NewReader(c.body)); err == nil {
			t.Fatalf("%s: 应报错", c.name)
		}
	}
}
