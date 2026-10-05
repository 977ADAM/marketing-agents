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

func (q *Query) RunEventsPage(ctx context.Context, id string, after int64, limit int) (trace.Page, error) {
	if store, ok := q.store.(trace.PagedStore); ok {
		return store.RunEventsPage(ctx, id, after, limit)
	}
	all, err := q.store.RunEvents(ctx, id, 0)
	if err != nil {
		return trace.Page{}, err
	}
	page := trace.Page{Total: len(all)}
	for _, row := range all {
		if row.Seq > after {
			page.Rows = append(page.Rows, row)
		}
	}
	if len(page.Rows) > limit {
		page.HasMore = true
		page.Rows = page.Rows[:limit]
	}
	if len(page.Rows) > 0 {
		page.NextSeq = page.Rows[len(page.Rows)-1].Seq
	}
	return page, nil
}
