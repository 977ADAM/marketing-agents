package reviewservice

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

// Store — хранение проверок текстов: порт у потребителя, реализация — адаптер.
//
// Суффикс Check сохранён для совместимости существующих портов и тестовых клиентов.
type Store interface {
	CreateCheck(ctx context.Context, clientID, briefText string) (string, error)
	MarkCheckRunning(ctx context.Context, id string) error
	SaveCheckProgress(ctx context.Context, id string, snap run.Snapshot) error
	CompleteCheck(ctx context.Context, id string, res review.Result) error
	FailCheck(ctx context.Context, id, msg string) error
	GetCheck(ctx context.Context, id string) (*review.Record, error)
	ListChecks(ctx context.Context, limit int) ([]review.Summary, error)
}

// CreationStore persists idempotency independently of the ordinary read/write port.
type CreationStore interface {
	LookupCreation(context.Context, string, string, string) (string, error)
	CreateOnce(context.Context, string, string, string, review.Request) (string, bool, error)
}
