// Package toolbudget accounts for physical tool requests, including retries.
package toolbudget

import (
	"context"
	"errors"
	"sync"
)

var ErrExhausted = errors.New("tool call budget exhausted")

type Budget struct {
	mu                         sync.Mutex
	max, used, initializations int
}
type contextKey struct{}

func New(ctx context.Context, max int) (context.Context, *Budget) {
	b := &Budget{max: max}
	return context.WithValue(ctx, contextKey{}, b), b
}
func Consume(ctx context.Context) error {
	b, _ := ctx.Value(contextKey{}).(*Budget)
	if b == nil {
		return nil
	}
	return b.Consume()
}
func (b *Budget) Consume() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.max {
		return ErrExhausted
	}
	b.used++
	return nil
}
func Initialization(ctx context.Context) {
	b, _ := ctx.Value(contextKey{}).(*Budget)
	if b != nil {
		b.mu.Lock()
		b.initializations++
		b.mu.Unlock()
	}
}
func (b *Budget) Used() int            { b.mu.Lock(); defer b.mu.Unlock(); return b.used }
func (b *Budget) Initializations() int { b.mu.Lock(); defer b.mu.Unlock(); return b.initializations }
