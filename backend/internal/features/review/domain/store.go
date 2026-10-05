package review

import (
	"errors"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
)

// ErrNotFound — проверки с таким id нет.
var ErrNotFound = errors.New("review not found")

// Record — сохранённая проверка текстов: бриф, отчёт и состояние прогона.
type Record struct {
	ResumeAvailable bool          `json:"resume_available"`
	ID              string        `json:"id"`
	ClientID        string        `json:"client_id"`
	Status          string        `json:"status"`
	BriefText       string        `json:"brief_text"`
	Result          *Result       `json:"result,omitempty"`
	Progress        *run.Snapshot `json:"progress,omitempty"`
	CostUSD         *float64      `json:"cost_usd,omitempty"`
	Error           string        `json:"error,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
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
