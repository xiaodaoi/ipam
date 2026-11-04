package dns

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
)

// 名单内容工具（M6-023）：CSV 导入导出 + 内置分类库。
// 两者都只处理 qname 类条目；response_ip 走 API 单条维护（CIDR 不适合 CSV 批量语义）。

// ParseEntryCSV 解析 CSV 文本（pattern[,action[,category]]）。
// 返回可入库条目与被跳过原因（最多保留 20 条，避免响应体过大）。
func ParseEntryCSV(content string) ([]Entry, []string) {
	content = strings.TrimPrefix(content, "\uFEFF")
	entries := []Entry{}
	skipped := []string{}
	note := func(reason string) {
		if len(skipped) < 20 {
			skipped = append(skipped, reason)
		}
	}

	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.LazyQuotes = true

	seen := map[string]bool{}
	line := 0
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		line++
		if len(row) == 0 {
			continue
		}
		pattern := strings.TrimSpace(row[0])
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		// 表头识别：首行首列恰为 pattern
		if line == 1 && strings.EqualFold(pattern, "pattern") {
			continue
		}
		action := "nxdomain"
		if len(row) > 1 {
			if a := strings.TrimSpace(row[1]); a != "" {
				action = a
			}
		}
		category := ""
		if len(row) > 2 {
			category = strings.TrimSpace(row[2])
		}
		if err := validateQnamePattern(pattern); err != nil {
			note(fmt.Sprintf("第 %d 行 %q：%v", line, pattern, err))
			continue
		}
		switch action {
		case "nxdomain", "drop", "tcp_only", "redirect":
		default:
			note(fmt.Sprintf("第 %d 行 %q：未知动作 %q", line, pattern, action))
			continue
		}
		key := strings.ToLower(pattern)
		if seen[key] {
			note(fmt.Sprintf("第 %d 行 %q：重复", line, pattern))
			continue
		}
		seen[key] = true
		entries = append(entries, Entry{
			TriggerType: "qname", Pattern: pattern, Action: action, Category: category,
		})
	}
	return entries, skipped
}

// validateQnamePattern 域名条目校验：仅最左标签可通配；字符集限 ASCII 域名与点。
func validateQnamePattern(p string) error {
	if len(p) > 253 {
		return fmt.Errorf("长度超过 253")
	}
	if strings.ContainsAny(p, " \t/://") {
		return fmt.Errorf("包含非法字符")
	}
	body := p
	if strings.HasPrefix(p, "*.") {
		body = p[2:]
	} else if strings.Contains(p, "*") {
		return fmt.Errorf("通配符仅允许出现在最左侧标签")
	}
	if body == "" {
		return fmt.Errorf("域名不能为空")
	}
	for _, r := range body {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
		default:
			return fmt.Errorf("包含非法字符 %q", r)
		}
	}
	if strings.HasPrefix(body, ".") || strings.HasSuffix(body, ".") || strings.Contains(body, "..") {
		return fmt.Errorf("域名标签为空")
	}
	return nil
}

// EntriesCSV 导出条目为 CSV（UTF-8 BOM，Excel 友好；按 pattern 排序）。
func EntriesCSV(entries []Entry) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"pattern", "action", "category"})
	for _, e := range entries {
		_ = w.Write([]string{e.Pattern, e.Action, e.Category})
	}
	w.Flush()
	return buf.Bytes()
}

// BuiltinPack 内置分类包（随产品分发，安装后成为 kind=builtin 名单）。
type BuiltinPack struct {
	Key         string
	Name        string
	Description string
	Categories  []string
	Patterns    []string
}

// builtinPacks 内置分类库种子。
// 说明：作为「开箱可用」基线，仅覆盖各分类最具代表性的域名；管理员可安装后增删，
// 或叠加订阅源扩充。不承诺覆盖率，避免给客户「全量拦截」的错误预期。
var builtinPacks = []BuiltinPack{
	{
		Key: "ads", Name: "广告拦截", Description: "常见展示广告与广告联盟域名（基线种子，可增删）",
		Categories: []string{"ads"},
		Patterns: []string{
			"*.doubleclick.net", "*.googlesyndication.com", "*.googleadservices.com",
			"*.adnxs.com", "*.adsrvr.org", "*.criteo.com", "*.taboola.com", "*.outbrain.com",
			"*.adcolony.com", "*.applovin.com", "*.unityads.unity3d.com", "*.mopub.com",
			"*.smartadserver.com", "*.pubmatic.com", "*.rubiconproject.com", "*.openx.net",
		},
	},
	{
		Key: "trackers", Name: "追踪与统计", Description: "行为分析、埋点与跨站追踪域名（基线种子）",
		Categories: []string{"tracking"},
		Patterns: []string{
			"*.google-analytics.com", "*.googletagmanager.com", "*.scorecardresearch.com",
			"*.quantserve.com", "*.hotjar.com", "*.mixpanel.com", "*.segment.io",
			"*.amplitude.com", "*.fullstory.com", "*.mouseflow.com", "*.crazyegg.com",
			"*.branch.io", "*.adjust.com", "*.appsflyer.com", "*.kochava.com",
		},
	},
	{
		Key: "gambling", Name: "赌博站点", Description: "在线博彩与赌场类域名（基线种子）",
		Categories: []string{"gambling"},
		Patterns: []string{
			"*.bet365.com", "*.1xbet.com", "*.pinnacle.com", "*.bwin.com",
			"*.williamhill.com", "*.draftkings.com", "*.fanduel.com", "*.betway.com",
			"*.unibet.com", "*.888casino.com", "*.pokerstars.com", "*.betfair.com",
		},
	},
	{
		Key: "adult", Name: "成人内容", Description: "成人内容站点（基线种子，按组织合规要求启用）",
		Categories: []string{"adult"},
		Patterns: []string{
			"*.pornhub.com", "*.xvideos.com", "*.xnxx.com", "*.redtube.com",
			"*.youporn.com", "*.brazzers.com", "*.onlyfans.com", "*.chaturbate.com",
		},
	},
	{
		Key: "social", Name: "社交媒体", Description: "通用社交与短视频平台（是否拦截取决于办公策略）",
		Categories: []string{"social"},
		Patterns: []string{
			"*.facebook.com", "*.instagram.com", "*.tiktok.com", "*.twitter.com",
			"*.x.com", "*.snapchat.com", "*.reddit.com", "*.weibo.com",
			"*.douyin.com", "*.kuaishou.com", "*.zhihu.com", "*.douban.com",
		},
	},
}

// BuiltinPacks 返回内置分类包目录（副本）。
func BuiltinPacks() []BuiltinPack {
	out := make([]BuiltinPack, len(builtinPacks))
	copy(out, builtinPacks)
	return out
}

// FindBuiltinPack 按 key 查找内置分类包。
func FindBuiltinPack(key string) (BuiltinPack, bool) {
	for _, p := range builtinPacks {
		if p.Key == key {
			return p, true
		}
	}
	return BuiltinPack{}, false
}
