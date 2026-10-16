package graph

import (
	"context"
	"sync"
	"time"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
)

// loader batches the ids requested by concurrently resolving fields into one
// fetch and caches results for the rest of the request. gqlgen resolves list
// items in parallel, so a page of 20 bookings costs one GetUsers call.
type loader[V any] struct {
	fetch func(ctx context.Context, ids []string) (map[string]V, error)
	wait  time.Duration
	max   int

	mu      sync.Mutex
	cache   map[string]*loaded[V]
	pending *pendingBatch[V]
}

type loaded[V any] struct {
	done chan struct{}
	val  V
	ok   bool
	err  error
}

type pendingBatch[V any] struct {
	ids     []string
	results []*loaded[V]
}

func newLoader[V any](fetch func(context.Context, []string) (map[string]V, error)) *loader[V] {
	return &loader[V]{fetch: fetch, wait: 2 * time.Millisecond, max: 200, cache: map[string]*loaded[V]{}}
}

// load returns the value for id; ok is false when it doesn't exist.
func (l *loader[V]) load(ctx context.Context, id string) (V, bool, error) {
	l.mu.Lock()
	r := l.enqueue(ctx, id)
	l.mu.Unlock()
	<-r.done
	return r.val, r.ok, r.err
}

// loadMany returns the values that exist for ids, in order. All of them join
// the same batch, instead of one batch round per id.
func (l *loader[V]) loadMany(ctx context.Context, ids []string) ([]V, error) {
	l.mu.Lock()
	rs := make([]*loaded[V], len(ids))
	for i, id := range ids {
		rs[i] = l.enqueue(ctx, id)
	}
	l.mu.Unlock()
	out := make([]V, 0, len(ids))
	for _, r := range rs {
		<-r.done
		if r.err != nil {
			return nil, r.err
		}
		if r.ok {
			out = append(out, r.val)
		}
	}
	return out, nil
}

// enqueue adds id to the pending batch, or returns its cached entry. The
// caller holds l.mu.
func (l *loader[V]) enqueue(ctx context.Context, id string) *loaded[V] {
	if r, hit := l.cache[id]; hit {
		return r
	}
	r := &loaded[V]{done: make(chan struct{})}
	l.cache[id] = r
	if l.pending == nil {
		b := &pendingBatch[V]{}
		l.pending = b
		time.AfterFunc(l.wait, func() { l.run(ctx, b) })
	}
	b := l.pending
	b.ids = append(b.ids, id)
	b.results = append(b.results, r)
	if len(b.ids) >= l.max {
		l.pending = nil
		go l.run(ctx, b)
	}
	return r
}

func (l *loader[V]) run(ctx context.Context, b *pendingBatch[V]) {
	l.mu.Lock()
	if l.pending == b {
		l.pending = nil
	}
	l.mu.Unlock()
	if len(b.ids) == 0 {
		return
	}
	vals, err := l.fetch(context.WithoutCancel(ctx), b.ids)
	for i, id := range b.ids {
		r := b.results[i]
		r.val, r.ok = vals[id]
		r.err = err
		close(r.done)
	}
}

// prime seeds the cache with a value fetched some other way.
func (l *loader[V]) prime(id string, v V) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, hit := l.cache[id]; hit {
		return
	}
	r := &loaded[V]{done: make(chan struct{}), val: v, ok: true}
	close(r.done)
	l.cache[id] = r
}

type loaders struct {
	users    *loader[*identityv1.User]
	campuses *loader[*identityv1.Campus]
	subjects *loader[*marketplacev1.Subject]
	profiles *loader[*marketplacev1.ProviderProfile]
	// Communities as the viewer sees them (with their role); see actor().
	communities *loader[*communityv1.Community]
}

func newLoaders(r *Resolver) *loaders {
	l := &loaders{
		users: newLoader(func(ctx context.Context, ids []string) (map[string]*identityv1.User, error) {
			res, err := r.Identity.GetUsers(ctx, &identityv1.GetUsersRequest{UserIds: ids})
			if err != nil {
				return nil, err
			}
			out := make(map[string]*identityv1.User, len(res.Users))
			for _, u := range res.Users {
				out[u.Id] = u
			}
			return out, nil
		}),
		campuses: newLoader(func(ctx context.Context, ids []string) (map[string]*identityv1.Campus, error) {
			res, err := r.Identity.GetCampuses(ctx, &identityv1.GetCampusesRequest{Ids: ids})
			if err != nil {
				return nil, err
			}
			out := make(map[string]*identityv1.Campus, len(res.Campuses))
			for _, c := range res.Campuses {
				out[c.Id] = c
			}
			return out, nil
		}),
		subjects: newLoader(func(ctx context.Context, ids []string) (map[string]*marketplacev1.Subject, error) {
			res, err := r.Market.GetSubjects(ctx, &marketplacev1.GetSubjectsRequest{Ids: ids})
			if err != nil {
				return nil, err
			}
			out := make(map[string]*marketplacev1.Subject, len(res.Subjects))
			for _, s := range res.Subjects {
				out[s.Id] = s
			}
			return out, nil
		}),
		profiles: newLoader(func(ctx context.Context, ids []string) (map[string]*marketplacev1.ProviderProfile, error) {
			// There is no batch RPC for profiles; fetch them concurrently.
			out := make(map[string]*marketplacev1.ProviderProfile, len(ids))
			var mu sync.Mutex
			var wg sync.WaitGroup
			var firstErr error
			for _, id := range ids {
				wg.Add(1)
				go func(id string) {
					defer wg.Done()
					p, err := r.Market.GetProviderProfile(ctx, &marketplacev1.GetProviderProfileRequest{Id: id})
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						out[id] = p
					} else if !isNotFound(err) && firstErr == nil {
						firstErr = err
					}
				}(id)
			}
			wg.Wait()
			return out, firstErr
		}),
	}
	l.communities = newCommunityLoader(r)
	return l
}

func newCommunityLoader(r *Resolver) *loader[*communityv1.Community] {
	return newLoader(func(ctx context.Context, ids []string) (map[string]*communityv1.Community, error) {
		a, err := r.actor(ctx)
		if err != nil {
			return nil, err
		}
		out := make(map[string]*communityv1.Community, len(ids))
		for _, id := range ids {
			c, err := r.CommunitySvc.GetCommunity(ctx, &communityv1.GetCommunityRequest{Actor: a, Id: id})
			if err == nil {
				out[id] = c
			} else if !isNotFound(err) {
				return nil, err
			}
		}
		return out, nil
	})
}

func loadersFrom(ctx context.Context) *loaders {
	l, _ := ctx.Value(loadersKey).(*loaders)
	return l
}
