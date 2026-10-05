package mock

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
)

// Reservation executes synchronously for service tests, without background work.
type Reservation struct{}

func (Reservation) Submit(task run.Task) error { task(context.Background()); return nil }
func (Reservation) Release()                   {}
