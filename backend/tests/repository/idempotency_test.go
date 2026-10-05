package repository_test

import (
	"context"
	"errors"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	"sync"
	"testing"
)

func TestConcurrentCreateOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ids := make(chan string, 2)
	created := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, isNew, err := s.campaigns.CreateOnce(ctx, "", "same-key", "hash", campaign.Brief{Product: "P"})
			ids <- id
			created <- isNew
			errs <- err
		}()
	}
	wg.Wait()
	a, b := <-ids, <-ids
	if a == "" || a != b {
		t.Fatalf("ids %q %q", a, b)
	}
	n := 0
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if <-created {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("created=%d", n)
	}
	if _, _, err := s.campaigns.CreateOnce(ctx, "", "same-key", "changed", campaign.Brief{Product: "Q"}); !errors.Is(err, run.ErrIdempotencyConflict) {
		t.Fatalf("conflict=%v", err)
	}
}
