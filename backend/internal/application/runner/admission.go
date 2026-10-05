package runner

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"sync"
)

// Admission separates capacity reservations from execution and shutdown accounting.
type Admission struct {
	mu             sync.Mutex
	capacity, used int
	stopping       bool
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

func NewAdmission(ctx context.Context, capacity int) *Admission {
	if capacity <= 0 {
		capacity = 64
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Admission{capacity: capacity, ctx: ctx, cancel: cancel}
}
func (a *Admission) Reserve(ctx context.Context) (run.Reservation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.stopping || a.ctx.Err() != nil {
		return nil, run.ErrStopping
	}
	if a.used >= a.capacity {
		return nil, run.ErrBusy
	}
	a.used++
	a.wg.Add(1)
	return &reservation{admission: a}, nil
}

type reservation struct {
	mu        sync.Mutex
	admission *Admission
	consumed  bool
}

func (r *reservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.consumed {
		r.consumed = true
		r.admission.release()
	}
}
func (a *Admission) release() { a.mu.Lock(); a.used--; a.mu.Unlock(); a.wg.Done() }
func (r *reservation) Submit(task run.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.consumed {
		return run.ErrStopping
	}
	r.consumed = true
	a := r.admission
	a.mu.Lock()
	stopping := a.stopping || a.ctx.Err() != nil
	a.mu.Unlock()
	if stopping {
		a.release()
		return run.ErrStopping
	}
	go func() { defer a.release(); task(a.ctx) }()
	return nil
}
func (a *Admission) Drain(ctx context.Context) error {
	a.mu.Lock()
	a.stopping = true
	a.mu.Unlock()
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
		a.cancel()
		return nil
	case <-ctx.Done():
		a.cancel()
		return ctx.Err()
	}
}
