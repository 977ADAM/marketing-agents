package run

import (
	"context"
	"errors"
	"time"
)

var ErrLeaseLost = errors.New("run ownership lease lost")

type Ownership struct {
	Owner   string
	Attempt int64
}
type ownershipKey struct{}

func WithOwnership(ctx context.Context, o Ownership) context.Context {
	return context.WithValue(ctx, ownershipKey{}, o)
}
func Owner(ctx context.Context) (Ownership, bool) {
	o, ok := ctx.Value(ownershipKey{}).(Ownership)
	return o, ok
}

type LeaseStore interface {
	AcquireLease(context.Context, string, string, time.Duration) (int64, bool, error)
	RenewLease(context.Context, string, string, int64, time.Duration) error
}
