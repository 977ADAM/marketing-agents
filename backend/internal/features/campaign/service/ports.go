package campaignservice

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
)

// Store — хранение кампаний: порт объявлен у потребителя (транспорт, сервис),
// реализация живёт в адаптере (internal/repository/mariadb).
type Store interface {
	Create(ctx context.Context, clientID string, b campaign.Brief) (string, error)
	MarkRunning(ctx context.Context, id string) error
	SaveProgress(ctx context.Context, id string, snap run.Snapshot) error
	Complete(ctx context.Context, id string, res campaign.Outcome) error
	Fail(ctx context.Context, id, msg string) error
	ListRecent(ctx context.Context, limit int) ([]campaign.Summary, error)
	Get(ctx context.Context, id string) (*campaign.Record, error)
}

// CreationStore persists idempotency independently of the ordinary read/write port.
type CreationStore interface {
	LookupCreation(context.Context, string, string, string) (string, error)
	CreateOnce(context.Context, string, string, string, campaign.Brief) (string, bool, error)
}
