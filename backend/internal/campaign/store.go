package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/977ADAM/marketing-agents/internal/run"
)

// ErrNotFound — кампании с таким id нет.
var ErrNotFound = errors.New("campaign not found")

// Record — сохранённая кампания: бриф, стратегия, статьи и состояние прогона.
type Record struct {
	ID           string        `json:"id"`
	ClientID     string        `json:"client_id"`
	Status       string        `json:"status"`
	Brief        Brief         `json:"brief"`
	Strategy     *Strategy     `json:"strategy,omitempty"`
	Deliverables []Deliverable `json:"deliverables,omitempty"`
	Progress     *run.Snapshot `json:"progress,omitempty"`
	CostUSD      *float64      `json:"cost_usd,omitempty"`
	Error        string        `json:"error,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// Summary — лёгкая сводка для списка истории (без strategy/deliverables/body).
type Summary struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Brief     Brief     `json:"brief"`
	CostUSD   *float64  `json:"cost_usd,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Outcome — итог прогона для записи в хранилище.
type Outcome struct {
	Strategy     Strategy
	Deliverables []Deliverable
	CostUSD      float64
}

// Store — хранение кампаний: порт объявлен у потребителя (транспорт, сервис),
// реализация живёт в адаптере (internal/store).
type Store interface {
	Create(ctx context.Context, clientID string, b Brief) (string, error)
	MarkRunning(ctx context.Context, id string) error
	SaveProgress(ctx context.Context, id string, snap run.Snapshot) error
	Complete(ctx context.Context, id string, res Outcome) error
	Fail(ctx context.Context, id, msg string) error
	ListRecent(ctx context.Context, limit int) ([]Summary, error)
	Get(ctx context.Context, id string) (*Record, error)
}
