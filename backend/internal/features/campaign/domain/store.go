package campaign

import (
	"errors"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
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
