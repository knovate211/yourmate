package graph

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoaderBatches(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(func(_ context.Context, ids []string) (map[string]string, error) {
		calls.Add(1)
		out := map[string]string{}
		for _, id := range ids {
			if id != "missing" {
				out[id] = "v" + id
			}
		}
		return out, nil
	})
	l.wait = 50 * time.Millisecond // generous, so a slow CI machine still batches
	ctx := context.Background()

	// Concurrent single loads and a loadMany share one fetch.
	var wg sync.WaitGroup
	for _, id := range []string{"1", "2", "3"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, ok, err := l.load(ctx, id); err != nil || !ok || v != "v"+id {
				t.Errorf("load %s = %q %v %v", id, v, ok, err)
			}
		}()
	}
	got, err := l.loadMany(ctx, []string{"3", "4", "missing", "5"})
	wg.Wait()
	if err != nil || len(got) != 3 || got[0] != "v3" || got[2] != "v5" {
		t.Fatalf("loadMany = %v %v", got, err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected one batched fetch, got %d", n)
	}

	// Cached ids cost nothing more.
	if _, err := l.loadMany(ctx, []string{"1", "4"}); err != nil || calls.Load() != 1 {
		t.Fatalf("cached loads refetched: calls=%d err=%v", calls.Load(), err)
	}
}
