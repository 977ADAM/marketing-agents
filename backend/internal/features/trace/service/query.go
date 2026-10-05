package traceservice

import (
	"context"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

type Query struct{ store trace.Store }

func NewQuery(store trace.Store) *Query { return &Query{store} }
func (q *Query) RunEvents(ctx context.Context, id string, limit int) ([]trace.Row, error) {
	return q.store.RunEvents(ctx, id, limit)
}
func (q *Query) RunEvent(ctx context.Context, id string, seq int64) (*trace.Row, error) {
	return q.store.RunEvent(ctx, id, seq)
}
