package run

import (
	"context"
	"errors"
	llm "github.com/977ADAM/marketing-agents/internal/core/llm"
	"time"
)

var ErrInvalidState = errors.New("run must be failed to resume")
var ErrResumeUnavailable = errors.New("saved input is unavailable for this run")

type CheckpointStore interface {
	LoadCheckpoint(context.Context, string, string, int, any) (bool, error)
	SaveCheckpoint(context.Context, string, string, int, any) error
}
type ResumeStore interface {
	CheckpointStore
	Requeue(context.Context, string) error
}
type Saved[T any] struct {
	Value T         `json:"value"`
	Usage llm.Usage `json:"usage"`
}
type RunSummary struct {
	CostKnown bool      `json:"cost_known"`
	CostUSD   float64   `json:"cost_usd"`
	Usage     llm.Usage `json:"usage"`
}

// Checkpoints is inert outside persisted runs, preserving standalone workflows.
type Checkpoints struct {
	Store CheckpointStore
	ID    string
}

func (c Checkpoints) Load(ctx context.Context, stage string, pos int, out any) (bool, error) {
	if c.Store == nil || c.ID == "" {
		return false, nil
	}
	return c.Store.LoadCheckpoint(ctx, c.ID, stage, pos, out)
}
func (c Checkpoints) Save(ctx context.Context, stage string, pos int, data any) error {
	if c.Store == nil || c.ID == "" {
		return nil
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return c.Store.SaveCheckpoint(final, c.ID, stage, pos, data)
}
