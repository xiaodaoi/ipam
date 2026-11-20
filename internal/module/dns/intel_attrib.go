package dns

import (
	"context"
	"log"
	"sort"
	"strings"
)

// 情报来源归属（M6-040 增强）：把一个被 RPZ 拦截的域名映射回命中的外部情报源。
//
// 归因不等于匹配顺序：unbound 命中哪个 rpz 记录由区顺序与记录顺序决定，
// 而日志里只有「域名被拦截」这一事实。这里按域名后缀由长到短命中：
// 先找精确域名，再逐级去掉最左标签，取「最具体」的一层来源；
// 同一层可能被多个源收录，则全部返回（逗号分隔），前端完整展示。
//
// 自定义封禁与内置规则不来自外部情报源，归属为空，由前端按 detail 的
// category 展示中性文案。

// domainAttrSource 索引中的源元数据（避免每条域名重复存字符串）。
type domainAttrSource struct {
	key  string
	name string
}

// domainAttribution 域名 → 源索引（懒构建，源变更后整体失效）。
type domainAttribution struct {
	sources  []domainAttrSource
	byDomain map[string][]uint16
}

func (a *domainAttribution) add(domain string, src uint16) {
	if domain == "" {
		return
	}
	list := a.byDomain[domain]
	for _, v := range list {
		if v == src {
			return
		}
	}
	a.byDomain[domain] = append(list, src)
}

// buildAttributionLocked 读取全部已启用源的域名缓存构建索引。
// 百万级 IOC 只存在于内存索引与磁盘缓存，不入库。
func (s *IntelService) buildAttributionLocked(ctx context.Context) *domainAttribution {
	idx := &domainAttribution{byDomain: make(map[string][]uint16, 4096)}
	list, err := s.store.ListSources(ctx)
	if err != nil {
		log.Printf("[intel] 构建来源归属索引失败（读取源列表）: %v", err)
		return idx
	}
	for _, src := range list {
		if !src.Enabled {
			continue
		}
		if len(idx.sources) >= 65535 {
			log.Printf("[intel] 来源数量超过归属索引上限，后续源不参与归属")
			break
		}
		pos := uint16(len(idx.sources))
		idx.sources = append(idx.sources, domainAttrSource{key: src.Key, name: src.Name})
		domains, rerr := readDomainCache(s.SourceDomainPath(src.Key))
		if rerr != nil {
			log.Printf("[intel] 读取源 %s 缓存失败（归属跳过）: %v", src.Key, rerr)
			continue
		}
		for _, d := range domains {
			d = normalizeDomain(strings.TrimPrefix(strings.TrimSpace(d), "*."))
			if d == "" {
				continue
			}
			idx.add(d, pos)
		}
	}
	return idx
}

// invalidateAttribution 源数据/元数据变化后作废索引，下次按需重建。
func (s *IntelService) invalidateAttribution() {
	s.attrMu.Lock()
	s.attr = nil
	s.attrMu.Unlock()
}

// AttributeDomain 返回域名命中的情报源 key 与显示名（逗号分隔）。
// 未命中（自定义封禁/内置规则/无启用源）返回空串。
func (s *IntelService) AttributeDomain(ctx context.Context, domain string) (keys, names string) {
	name := normalizeDomain(domain)
	if name == "" {
		return "", ""
	}

	s.attrMu.Lock()
	idx := s.attr
	if idx == nil {
		idx = s.buildAttributionLocked(ctx)
		s.attr = idx
	}
	s.attrMu.Unlock()

	if len(idx.sources) == 0 {
		return "", ""
	}
	// 逐级后缀查找：最具体的命中优先（如 a.b.example.com → b.example.com → example.com）。
	for n := name; n != ""; {
		if hits := idx.byDomain[n]; len(hits) > 0 {
			srcs := make([]domainAttrSource, 0, len(hits))
			for _, h := range hits {
				srcs = append(srcs, idx.sources[h])
			}
			// 按 key 排序：同一域名被多源收录时输出稳定，便于去重与比对。
			sort.Slice(srcs, func(i, j int) bool { return srcs[i].key < srcs[j].key })
			ks := make([]string, 0, len(srcs))
			ns := make([]string, 0, len(srcs))
			for _, src := range srcs {
				ks = append(ks, src.key)
				ns = append(ns, src.name)
			}
			return strings.Join(ks, ","), strings.Join(ns, ",")
		}
		i := strings.IndexByte(n, '.')
		if i < 0 {
			break
		}
		n = n[i+1:]
	}
	return "", ""
}
