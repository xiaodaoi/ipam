package alerts

import (
	"context"
	"sort"
	"time"

	guuid "github.com/google/uuid"
)

func (r *MemRepo) ListPolicies(context.Context) ([]Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Policy, 0, len(r.policies))
	for _, p := range r.policies {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScopeType != out[j].ScopeType {
			return out[i].ScopeType < out[j].ScopeType
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (r *MemRepo) CreatePolicy(_ context.Context, p Policy) (Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.policies {
		if x.ScopeType == p.ScopeType && x.ScopeKey == p.ScopeKey {
			return Policy{}, ErrConflict
		}
	}
	if p.ID == "" {
		p.ID = guuid.NewString()
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	r.policies[p.ID] = p
	return p, nil
}

func (r *MemRepo) UpdatePolicy(_ context.Context, p Policy) (Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.policies[p.ID]
	if !ok {
		return Policy{}, ErrNotFound
	}
	p.CreatedAt, p.CreatedBy = cur.CreatedAt, cur.CreatedBy
	p.UpdatedAt = time.Now().UTC()
	r.policies[p.ID] = p
	return p, nil
}

func (r *MemRepo) DeletePolicy(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.policies[id]; !ok {
		return ErrNotFound
	}
	delete(r.policies, id)
	return nil
}

func (r *MemRepo) SeedPolicies(_ context.Context, ps []Policy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range ps {
		exists := false
		for _, x := range r.policies {
			if x.ScopeType == p.ScopeType && x.ScopeKey == p.ScopeKey {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if p.ID == "" {
			p.ID = guuid.NewString()
		}
		now := time.Now().UTC()
		p.CreatedAt, p.UpdatedAt = now, now
		p.CreatedBy, p.UpdatedBy = "system", "system"
		r.policies[p.ID] = p
	}
	return nil
}

func (r *MemRepo) ListSilences(context.Context) ([]Silence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Silence, 0, len(r.silences))
	for _, s := range r.silences {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.After(out[j].StartsAt) })
	return out, nil
}

func (r *MemRepo) CreateSilence(_ context.Context, s Silence) (Silence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.ID == "" {
		s.ID = guuid.NewString()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	r.silences[s.ID] = s
	return s, nil
}

func (r *MemRepo) DeleteSilence(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.silences[id]; !ok {
		return ErrNotFound
	}
	delete(r.silences, id)
	return nil
}

func (r *MemRepo) ListChannels(context.Context) ([]Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Channel, 0, len(r.channels))
	for _, ch := range r.channels {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *MemRepo) GetChannel(_ context.Context, id string) (Channel, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.channels[id]
	return ch, ok, nil
}

func (r *MemRepo) CreateChannel(_ context.Context, ch Channel) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch.ID == "" {
		ch.ID = guuid.NewString()
	}
	now := time.Now().UTC()
	ch.CreatedAt, ch.UpdatedAt = now, now
	r.channels[ch.ID] = ch
	return ch, nil
}

func (r *MemRepo) UpdateChannel(_ context.Context, ch Channel) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.channels[ch.ID]
	if !ok {
		return Channel{}, ErrNotFound
	}
	ch.CreatedAt = cur.CreatedAt
	ch.UpdatedAt = time.Now().UTC()
	r.channels[ch.ID] = ch
	return ch, nil
}

func (r *MemRepo) DeleteChannel(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.channels[id]; !ok {
		return ErrNotFound
	}
	delete(r.channels, id)
	return nil
}

func (r *MemRepo) CreateDelivery(_ context.Context, d Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.ID == "" {
		d.ID = guuid.NewString()
	}
	if d.SentAt.IsZero() {
		d.SentAt = time.Now().UTC()
	}
	d.CreatedAt = time.Now().UTC()
	r.deliveries = append(r.deliveries, d)
	return nil
}

func (r *MemRepo) PurgeDeliveries(_ context.Context, before time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.deliveries[:0]
	removed := 0
	for _, d := range r.deliveries {
		if d.SentAt.Before(before) {
			removed++
			continue
		}
		kept = append(kept, d)
	}
	r.deliveries = kept
	return removed, nil
}

func (r *MemRepo) ListDeliveries(_ context.Context, f DeliveryFilter) ([]Delivery, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f.Limit <= 0 {
		f.Limit = 50
	}
	all := make([]Delivery, 0, len(r.deliveries))
	for _, d := range r.deliveries {
		if f.ChannelID != "" && d.ChannelID != f.ChannelID {
			continue
		}
		if f.Status != "" && d.Status != f.Status {
			continue
		}
		all = append(all, d)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].SentAt.After(all[j].SentAt) })
	total := len(all)
	if len(all) > f.Limit {
		all = all[:f.Limit]
	}
	return all, total, nil
}
