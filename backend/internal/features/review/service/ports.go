package reviewservice

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

// Store — хранение проверок текстов: порт у потребителя, реализация — адаптер.
//
// Имена методов с суффиксом Check (CreateCheck, GetCheck…) — плата за то, что
// один адаптер (internal/repository/mariadb) реализует и campaignservice.Store, и reviewservice.Store:
// одноимённые методы с разными подписями в Go несовместимы.
type Store interface {
	CreateCheck(ctx context.Context, clientID, briefText string) (string, error)
	MarkCheckRunning(ctx context.Context, id string) error
	SaveCheckProgress(ctx context.Context, id string, snap run.Snapshot) error
	CompleteCheck(ctx context.Context, id string, res review.Result) error
	FailCheck(ctx context.Context, id, msg string) error
	GetCheck(ctx context.Context, id string) (*review.Record, error)
	ListChecks(ctx context.Context, limit int) ([]review.Summary, error)
}
