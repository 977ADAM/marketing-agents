package corellm

import "context"

type UsageStore interface {
	AppendUsage(context.Context, string, UsageEntry) error
	UsageEntries(context.Context, string) ([]UsageEntry, error)
}
type usageSinkKey struct{}
type UsageSink func(context.Context, UsageEntry) error

func WithUsageSink(ctx context.Context, sink UsageSink) context.Context {
	return context.WithValue(ctx, usageSinkKey{}, sink)
}
func RecordUsage(ctx context.Context, e UsageEntry) error {
	sink, _ := ctx.Value(usageSinkKey{}).(UsageSink)
	if sink != nil {
		return sink(ctx, e)
	}
	return nil
}
