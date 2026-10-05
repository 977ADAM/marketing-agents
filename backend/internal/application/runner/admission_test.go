package runner_test

import (
	"context"
	"errors"
	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"testing"
	"time"
)

func TestAdmissionIsNonblocking(t *testing.T) {
	a := runner.NewAdmission(context.Background(), 1)
	slot, err := a.Reserve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Reserve(context.Background())
	if !errors.Is(err, run.ErrBusy) {
		t.Fatalf("busy=%v", err)
	}
	slot.Release()
	slot, err = a.Reserve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	slot.Release()
}
func TestCancelledContextDoesNotReserve(t *testing.T) {
	a := runner.NewAdmission(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Reserve(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
func TestDrainCancelsWorkerAndRejectsSubmit(t *testing.T) {
	a := runner.NewAdmission(context.Background(), 2)
	slot, _ := a.Reserve(context.Background())
	started := make(chan struct{})
	cancelled := make(chan struct{})
	if err := slot.Submit(func(ctx context.Context) { close(started); <-ctx.Done(); close(cancelled) }); err != nil {
		t.Fatal(err)
	}
	<-started
	pending, _ := a.Reserve(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := a.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain=%v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("worker not cancelled")
	}
	if err := pending.Submit(func(context.Context) {}); !errors.Is(err, run.ErrStopping) {
		t.Fatalf("submit=%v", err)
	}
}
