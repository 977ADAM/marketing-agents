package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/review"
)

// Review — модель строки проверки текстов для API.
type Review struct {
	ID        string                 `json:"id"`
	ClientID  string                 `json:"client_id"`
	Status    string                 `json:"status"`
	BriefText string                 `json:"brief_text"`
	Result    *review.Result         `json:"result,omitempty"`
	Progress  *orchestrator.Snapshot `json:"progress,omitempty"`
	CostUSD   *float64               `json:"cost_usd,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// ReviewSummary — лёгкая сводка для списка истории проверок.
// BriefTitle — первая строка брифа: заголовок для списка заполняет слой API,
// чтобы фронт ничего не вычислял сам.
type ReviewSummary struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	BriefText  string    `json:"brief_text"`
	BriefTitle string    `json:"brief_title,omitempty"`
	CostUSD    *float64  `json:"cost_usd,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// CreateReview вставляет проверку в статусе pending и возвращает её id.
func (s *Store) CreateReview(ctx context.Context, clientID, briefText string) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO reviews (id, client_id, status, brief_text) VALUES (?, ?, 'pending', ?)`,
		id, clientID, briefText)
	return id, err
}

// MarkReviewRunning переводит проверку в running.
func (s *Store) MarkReviewRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE reviews SET status='running', updated_at=`+nowExpr+` WHERE id=?`, id)
	return err
}

// SaveReviewProgress сохраняет снимок прогресса проверки (перезаписывает прошлый).
func (s *Store) SaveReviewProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := s.db.ExecContext(ctx,
		`UPDATE reviews SET progress=?, updated_at=`+nowExpr+` WHERE id=?`, string(b), id)
	return err
}

// CompleteReview сохраняет результат и переводит проверку в done.
func (s *Store) CompleteReview(ctx context.Context, id string, res review.Result) error {
	resultJSON, _ := json.Marshal(res)
	_, err := s.db.ExecContext(ctx,
		`UPDATE reviews SET status='done', result=?, cost_usd=?, updated_at=`+nowExpr+` WHERE id=?`,
		string(resultJSON), res.CostUSD, id)
	return err
}

// FailReview переводит проверку в failed с текстом ошибки.
func (s *Store) FailReview(ctx context.Context, id, msg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE reviews SET status='failed', error=?, updated_at=`+nowExpr+` WHERE id=?`, msg, id)
	return err
}

// GetReview читает проверку вместе с результатом.
func (s *Store) GetReview(ctx context.Context, id string) (*Review, error) {
	var r Review
	var resultJSON, progressJSON []byte
	var cost *float64
	var errText *string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, client_id, status, brief_text, result, cost_usd, error, progress, created_at, updated_at
		 FROM reviews WHERE id=?`, id).
		Scan(&r.ID, &r.ClientID, &r.Status, &r.BriefText, &resultJSON, &cost, &errText, &progressJSON, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(resultJSON) > 0 {
		var res review.Result
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, brief_text, cost_usd, created_at
		 FROM reviews ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
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
