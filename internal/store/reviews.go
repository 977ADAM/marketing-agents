package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/jackc/pgx/v5"
)

// Review — модель строки проверки текстов для API.
type Review struct {
	ID        string                       `json:"id"`
	ClientID  string                       `json:"client_id"`
	Status    string                       `json:"status"`
	BriefText string                       `json:"brief_text"`
	Result    *orchestrator.ReviewResult   `json:"result,omitempty"`
	Progress  *orchestrator.Snapshot       `json:"progress,omitempty"`
	CostUSD   *float64                     `json:"cost_usd,omitempty"`
	Error     string                       `json:"error,omitempty"`
	CreatedAt time.Time                    `json:"created_at"`
	UpdatedAt time.Time                    `json:"updated_at"`
}

// ReviewSummary — лёгкая сводка для списка истории проверок.
type ReviewSummary struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	BriefText string    `json:"brief_text"`
	CostUSD   *float64  `json:"cost_usd,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateReview вставляет проверку в статусе pending и возвращает её id.
func (s *Store) CreateReview(ctx context.Context, clientID, briefText string) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reviews (id, client_id, status, brief_text) VALUES ($1, $2, 'pending', $3)`,
		id, clientID, briefText)
	return id, err
}

// MarkReviewRunning переводит проверку в running.
func (s *Store) MarkReviewRunning(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE reviews SET status='running', updated_at=now() WHERE id=$1`, id)
	return err
}

// SaveReviewProgress сохраняет снимок прогресса проверки (перезаписывает прошлый).
func (s *Store) SaveReviewProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := s.pool.Exec(ctx,
		`UPDATE reviews SET progress=$2, updated_at=now() WHERE id=$1`, id, b)
	return err
}

// CompleteReview сохраняет результат и переводит проверку в done.
func (s *Store) CompleteReview(ctx context.Context, id string, res orchestrator.ReviewResult) error {
	resultJSON, _ := json.Marshal(res)
	_, err := s.pool.Exec(ctx,
		`UPDATE reviews SET status='done', result=$2, cost_usd=$3, updated_at=now() WHERE id=$1`,
		id, resultJSON, res.CostUSD)
	return err
}

// FailReview переводит проверку в failed с текстом ошибки.
func (s *Store) FailReview(ctx context.Context, id, msg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE reviews SET status='failed', error=$2, updated_at=now() WHERE id=$1`, id, msg)
	return err
}

// GetReview читает проверку вместе с результатом.
func (s *Store) GetReview(ctx context.Context, id string) (*Review, error) {
	var r Review
	var resultJSON, progressJSON []byte
	var cost *float64
	var errText *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, status, brief_text, result, cost_usd, error, progress, created_at, updated_at
		 FROM reviews WHERE id=$1`, id).
		Scan(&r.ID, &r.ClientID, &r.Status, &r.BriefText, &resultJSON, &cost, &errText, &progressJSON, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(resultJSON) > 0 {
		var res orchestrator.ReviewResult
		if json.Unmarshal(resultJSON, &res) == nil {
			r.Result = &res
		}
	}
	r.CostUSD = cost
	if errText != nil {
		r.Error = *errText
	}
	if len(progressJSON) > 0 {
		var snap orchestrator.Snapshot
		if json.Unmarshal(progressJSON, &snap) == nil {
			r.Progress = &snap
		}
	}
	return &r, nil
}

// ListReviews возвращает до limit последних проверок, новые сверху.
func (s *Store) ListReviews(ctx context.Context, limit int) ([]ReviewSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, status, brief_text, cost_usd, created_at
		 FROM reviews ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ReviewSummary, 0, limit)
	for rows.Next() {
		var r ReviewSummary
		var cost *float64
		if err := rows.Scan(&r.ID, &r.Status, &r.BriefText, &cost, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CostUSD = cost
		out = append(out, r)
	}
	return out, rows.Err()
}
