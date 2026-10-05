package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
)

// Reviews — хранилище reviews поверх общего соединения.
type Reviews struct{ db *sql.DB }

// NewReviews оборачивает соединение: сам SQL живёт в этом файле.
func NewReviews(db *sql.DB) *Reviews { return &Reviews{db: db} }

// CreateCheck вставляет проверку в статусе pending и возвращает её id.
func (rs *Reviews) CreateCheck(ctx context.Context, clientID, briefText string) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
	_, err := rs.db.ExecContext(ctx,
		`INSERT INTO reviews (id, client_id, status, brief_text) VALUES (?, ?, 'pending', ?)`,
		id, clientID, briefText)
	return id, err
}

// MarkCheckRunning переводит проверку в running.
func (rs *Reviews) MarkCheckRunning(ctx context.Context, id string) error {
	_, err := rs.db.ExecContext(ctx,
		`UPDATE reviews SET status='running', updated_at=`+nowExpr+` WHERE id=?`, id)
	return err
}

// SaveCheckProgress сохраняет снимок прогресса проверки (перезаписывает прошлый).
func (rs *Reviews) SaveCheckProgress(ctx context.Context, id string, snap run.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := rs.db.ExecContext(ctx,
		`UPDATE reviews SET progress=?, updated_at=`+nowExpr+` WHERE id=?`, string(b), id)
	return err
}

// CompleteCheck сохраняет результат и переводит проверку в done.
func (rs *Reviews) CompleteCheck(ctx context.Context, id string, res review.Result) error {
	resultJSON, _ := json.Marshal(res)
	_, err := rs.db.ExecContext(ctx,
		`UPDATE reviews SET status='done', result=?, cost_usd=?, updated_at=`+nowExpr+` WHERE id=?`,
		string(resultJSON), res.CostUSD, id)
	return err
}

// FailCheck переводит проверку в failed с текстом ошибки.
func (rs *Reviews) FailCheck(ctx context.Context, id, msg string) error {
	_, err := rs.db.ExecContext(ctx,
		`UPDATE reviews SET status='failed', error=?, updated_at=`+nowExpr+` WHERE id=?`, msg, id)
	return err
}

// GetCheck читает проверку вместе с результатом.
func (rs *Reviews) GetCheck(ctx context.Context, id string) (*review.Record, error) {
	var r review.Record
	var resultJSON, progressJSON []byte
	var cost *float64
	var errText *string
	err := rs.db.QueryRowContext(ctx,
		`SELECT id, client_id, status, brief_text, result, cost_usd, error, progress, created_at, updated_at
		 FROM reviews WHERE id=?`, id).
		Scan(&r.ID, &r.ClientID, &r.Status, &r.BriefText, &resultJSON, &cost, &errText, &progressJSON, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, review.ErrNotFound
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
		var snap run.Snapshot
		if json.Unmarshal(progressJSON, &snap) == nil {
			r.Progress = &snap
		}
	}
	return &r, nil
}

// ListChecks возвращает до limit последних проверок, новые сверху.
// seq — порядок вставки: тайбрейкер для записей с одинаковым created_at.
func (rs *Reviews) ListChecks(ctx context.Context, limit int) ([]review.Summary, error) {
	rows, err := rs.db.QueryContext(ctx,
		`SELECT id, status, brief_text, cost_usd, created_at
		 FROM reviews ORDER BY created_at DESC, seq DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]review.Summary, 0, limit)
	for rows.Next() {
		var r review.Summary
		var cost *float64
		if err := rows.Scan(&r.ID, &r.Status, &r.BriefText, &cost, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CostUSD = cost
		out = append(out, r)
	}
	return out, rows.Err()
}
