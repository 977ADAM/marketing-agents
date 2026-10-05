package review

import (
	"context"
	"errors"
	"time"

	"github.com/977ADAM/marketing-agents/internal/run"
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
// Имена методов с суффиксом Review (CreateReview, GetReview…) — временные: один
// тип-адаптер реализует и campaign.Store, и review.Store, а同名 методы с разными
// подписями в Go несовместимы. Суффиксы уйдут, когда адаптер разъедется по
// сущностям (шаг 3 плана: sqlite/campaign.go и sqlite/review.go).
type Store interface {
	CreateReview(ctx context.Context, clientID, briefText string) (string, error)
	MarkReviewRunning(ctx context.Context, id string) error
	SaveReviewProgress(ctx context.Context, id string, snap run.Snapshot) error
	CompleteReview(ctx context.Context, id string, res Result) error
	FailReview(ctx context.Context, id, msg string) error
	GetReview(ctx context.Context, id string) (*Record, error)
	ListReviews(ctx context.Context, limit int) ([]Summary, error)
}
