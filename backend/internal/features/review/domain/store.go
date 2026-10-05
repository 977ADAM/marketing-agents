package review

import (
	"context"
	"errors"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
)

// ErrNotFound — проверки с таким id нет.
var ErrNotFound = errors.New("review not found")

// Record — сохранённая проверка текстов: бриф, отчёт и состояние прогона.
type Record struct {
	ID        string        `json:"id"`
	ClientID  string        `json:"client_id"`
	Status    string        `json:"status"`
	BriefText string        `json:"brief_text"`
	Result    *Result       `json:"result,omitempty"`
	Progress  *run.Snapshot `json:"progress,omitempty"`
	CostUSD   *float64      `json:"cost_usd,omitempty"`
	Error     string        `json:"error,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Summary — лёгкая сводка для списка истории проверок.
// BriefTitle — первая строка брифа: её заполняет слой API, чтобы фронт ничего не
// вычислял сам.
type Summary struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	BriefText  string    `json:"brief_text"`
	BriefTitle string    `json:"brief_title,omitempty"`
	CostUSD    *float64  `json:"cost_usd,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Store — хранение проверок текстов: порт у потребителя, реализация — адаптер.
//
// Имена методов с суффиксом Check (CreateCheck, GetCheck…) — плата за то, что
// один адаптер (internal/repository/mariadb) реализует и campaign.Store, и review.Store:
// одноимённые методы с разными подписями в Go несовместимы.
type Store interface {
	CreateCheck(ctx context.Context, clientID, briefText string) (string, error)
	MarkCheckRunning(ctx context.Context, id string) error
	SaveCheckProgress(ctx context.Context, id string, snap run.Snapshot) error
	CompleteCheck(ctx context.Context, id string, res Result) error
	FailCheck(ctx context.Context, id, msg string) error
	GetCheck(ctx context.Context, id string) (*Record, error)
	ListChecks(ctx context.Context, limit int) ([]Summary, error)
}
