package alerts

import (
	"context"
	"sort"
	"sync"
	"time"

	guuid "github.com/google/uuid"
)

// MemRepo 内存仓储（开发/单测路径；同时实现 AdminRepo）。
type MemRepo struct {
	mu         sync.Mutex
	byKey      map[string]Alert
	policies   map[string]Policy
	silences   map[string]Silence
	channels   map[string]Channel
	deliveries []Delivery
}

// NewMemRepo 构造。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		byKey:    map[string]Alert{},
		policies: map[string]Policy{},
		silences: map[string]Silence{},
		channels: map[string]Channel{},
	}
}

func (r *MemRepo) snapshot(f Filter) []Alert {
	out := make([]Alert, 0, len(r.byKey))
	for _, a := range r.byKey {
		if matchFilter(a, f) {
			out = append(out, a)
		}
	}
	sortAlerts(out)
	return out
}

// List 内存实现。
func (r *MemRepo) List(_ context.Context, f Filter) ([]Alert, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.snapshot(f)
	total := len(all)
	start := (f.Page - 1) * f.PageSize
	if start > total {
		start = total
	}
	end := start + f.PageSize
	if end > total {
		end = total
	}
	return append([]Alert{}, all[start:end]...), total, nil
}

// Stats 内存实现。
func (r *MemRepo) Stats(_ context.Context, f Filter) (Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.snapshot(f)
	st := Stats{BySeverity: map[string]int{}, ByKind: map[string]int{},
		ByIntelSource: map[string]int{}, Bucket: f.Bucket}
	doms := map[string]int{}
	clients := map[string]int{}
	bucket := map[string]int{}
	notifiedBucket := map[string]int{}
	sources := map[string]*SourceStat{}
	loc := time.UTC
	for _, a := range all {
		st.Total++
		switch a.Status {
		case StatusOpen:
			st.Open++
		case StatusAcked:
			st.Acked++
		case StatusResolved:
			st.Resolved++
		}
		if a.Notify {
			st.Notified++
		} else {
			st.Archived++
		}
		if a.SuppressedBy != "" {
			st.Suppressed++
		}
		if a.Severity == SeverityCritical && !a.Notify {
			st.CriticalSuppressed++
		}
		st.BySeverity[a.Severity]++
		st.ByKind[a.Kind]++
		if a.Domain != "" {
			doms[a.Domain] += a.Count
		}
		if a.ClientIP != "" {
			clients[a.ClientIP] += a.Count
		}
		for _, name := range splitIntelSources(a.IntelSource, a.IntelSourceName) {
			st.ByIntelSource[name]++
		}
		sk := a.IntelSourceName
		if sk == "" {
			sk = a.Component
		}
		if sk == "" {
			sk = a.Source
		}
		s := sources[sk]
		if s == nil {
			s = &SourceStat{Key: sk}
			sources[sk] = s
		}
		s.Recorded++
		if a.Notify {
			s.Notified++
		}
		key := a.LastSeen.UTC().Format("2006-01-02T15")
		if f.Bucket != "hour" {
			key = a.LastSeen.UTC().Format("2006-01-02")
		}
		bucket[key]++
		if a.Notify {
			notifiedBucket[key]++
		}
	}
	topN := func(m map[string]int) []Aggregate {
		out := make([]Aggregate, 0, len(m))
		for k, v := range m {
			out = append(out, Aggregate{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count != out[j].Count {
				return out[i].Count > out[j].Count
			}
			return out[i].Key < out[j].Key
		})
		if len(out) > 10 {
			out = out[:10]
		}
		return out
	}
	st.TopDomains = topN(doms)
	st.TopClients = topN(clients)
	for _, s := range sources {
		st.BySource = append(st.BySource, *s)
	}
	sort.Slice(st.BySource, func(i, j int) bool {
		if st.BySource[i].Recorded != st.BySource[j].Recorded {
			return st.BySource[i].Recorded > st.BySource[j].Recorded
		}
		return st.BySource[i].Key < st.BySource[j].Key
	})
	if len(st.BySource) > 10 {
		st.BySource = st.BySource[:10]
	}
	keys := make([]string, 0, len(bucket))
	for k := range bucket {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		layout := "2006-01-02T15"
		if f.Bucket != "hour" {
			layout = "2006-01-02"
		}
		ts, _ := time.ParseInLocation(layout, k, loc)
		st.Trend = append(st.Trend, TrendPoint{TS: ts, Count: bucket[k]})
		st.NotifiedTrend = append(st.NotifiedTrend, TrendPoint{TS: ts, Count: notifiedBucket[k]})
	}
	return st, nil
}

// Unread 内存实现（仅 notify=true）。
func (r *MemRepo) Unread(_ context.Context, limit int) (Unread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Unread{Items: []Alert{}}
	all := make([]Alert, 0, len(r.byKey))
	for _, a := range r.byKey {
		if a.Status == StatusOpen && a.Notify {
			all = append(all, a)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := severityRank(all[i].Severity), severityRank(all[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return all[i].LastSeen.After(all[j].LastSeen)
	})
	out.Count = len(all)
	if len(all) > limit {
		all = all[:limit]
	}
	out.Items = append(out.Items, all...)
	return out, nil
}

// Ack 内存实现。
func (r *MemRepo) Ack(_ context.Context, ids []string, actor string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	n := 0
	for k, a := range r.byKey {
		if want[a.ID] && a.Status == StatusOpen {
			t := at
			a.Status = StatusAcked
			a.AckedAt = &t
			a.AckedBy = actor
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// AckAll 内存实现。
func (r *MemRepo) AckAll(_ context.Context, actor string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, a := range r.byKey {
		if a.Status == StatusOpen {
			t := at
			a.Status = StatusAcked
			a.AckedAt = &t
			a.AckedBy = actor
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// Upsert 内存实现：稳定身份 + 冷却/重复提醒。
func (r *MemRepo) Upsert(_ context.Context, ev Event) (Alert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byKey[ev.DedupKey]
	now := time.Now().UTC()
	reopened := false
	justNotified := false
	if !ok {
		a = Alert{
			ID:        guuid.NewString(),
			DedupKey:  ev.DedupKey,
			CreatedAt: ev.FirstSeen,
			Status:    StatusOpen,
		}
		reopened = true
	} else if a.Status == StatusResolved {
		a.Status = StatusOpen
		a.AckedAt = nil
		a.AckedBy = ""
		a.ResolvedAt = nil
		reopened = true
	} else if a.Status == StatusAcked {
		repeat := a.RepeatAfterSec > 0 && !ev.LastSeen.Before(a.LastSeen.Add(time.Duration(a.RepeatAfterSec)*time.Second))
		spike := a.Count > 0 && ev.Count >= a.Count*10
		if repeat || spike {
			a.Status = StatusOpen
			a.AckedAt = nil
			a.AckedBy = ""
			a.ResolvedAt = nil
			reopened = true
		}
	}

	a.Fingerprint, a.Source, a.Kind, a.Severity = ev.Fingerprint, ev.Source, ev.Kind, ev.Severity
	a.Title, a.Message, a.Detail = ev.Title, ev.Message, ev.Detail
	a.ClientIP, a.ClientMAC, a.Domain, a.Component = ev.ClientIP, ev.ClientMAC, ev.Domain, ev.Component
	a.IntelSource, a.IntelSourceName = ev.IntelSource, ev.IntelSourceName
	a.AlertClass, a.PolicyID, a.GroupKey = ev.AlertClass, ev.PolicyID, ev.GroupKey
	a.SuppressedBy, a.SilenceID, a.Decision = ev.SuppressedBy, ev.SilenceID, ev.Decision
	a.RepeatAfterSec, a.ResolveAfterSec = ev.RepeatAfterSec, ev.ResolveAfterSec
	if !ev.WindowStart.IsZero() {
		a.WindowStart = ev.WindowStart
	}
	if reopened {
		a.Count = ev.Count
		a.FirstSeen, a.LastSeen = ev.FirstSeen, ev.LastSeen
		a.Notify = ev.Notify
		if ev.Notify {
			a.NotifyCount++
			a.LastNotifiedAt = &now
			justNotified = true
		}
	} else {
		a.Notify = ev.Notify
		a.Count = maxInt(a.Count, ev.Count)
		if ev.FirstSeen.Before(a.FirstSeen) {
			a.FirstSeen = ev.FirstSeen
		}
		if ev.LastSeen.After(a.LastSeen) {
			a.LastSeen = ev.LastSeen
		}
	}
	a.JustNotified = justNotified
	r.byKey[ev.DedupKey] = a
	return a, nil
}

// Resolve 内存实现。
func (r *MemRepo) Resolve(_ context.Context, dedupKey string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byKey[dedupKey]
	if !ok || a.Status == StatusResolved {
		return false, nil
	}
	t := at
	a.Status = StatusResolved
	a.ResolvedAt = &t
	r.byKey[dedupKey] = a
	return true, nil
}

// ResolveStaleSystem 内存实现。
func (r *MemRepo) ResolveStaleSystem(_ context.Context, managedKinds, activeKeys []string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	managed := map[string]bool{}
	for _, k := range managedKinds {
		managed[k] = true
	}
	active := map[string]bool{}
	for _, k := range activeKeys {
		active[k] = true
	}
	n := 0
	for k, a := range r.byKey {
		if a.Source == SourceSystem && a.Status != StatusResolved && managed[a.Kind] && !active[a.DedupKey] {
			t := at
			a.Status = StatusResolved
			a.ResolvedAt = &t
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// AutoResolveDNS 内存实现。
func (r *MemRepo) AutoResolveDNS(_ context.Context, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, a := range r.byKey {
		if a.Source != SourceDNS || a.Status == StatusResolved || a.ResolveAfterSec <= 0 {
			continue
		}
		if a.LastSeen.Add(time.Duration(a.ResolveAfterSec) * time.Second).Before(now) {
			t := now
			a.Status = StatusResolved
			a.ResolvedAt = &t
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// Purge 内存实现。
func (r *MemRepo) Purge(_ context.Context, before time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, a := range r.byKey {
		if a.LastSeen.Before(before) {
			delete(r.byKey, k)
			n++
		}
	}
	return n, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
