// Package sqlite — адаптер хранения на SQLite (modernc.org/sqlite, чистый Go).
//
// Здесь только SQL и перевод строк в доменные типы: порты объявлены в доменных
// пакетах (campaign.Store, review.Store, trace.Store, trace.Sink), а имена файлов
// — по сущности, а не по слою.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/run"
)

// Campaigns — хранилище campaigns поверх общего соединения.
type Campaigns struct{ db *sql.DB }

// NewCampaigns оборачивает соединение: сам SQL живёт в этом файле.
func NewCampaigns(db *sql.DB) *Campaigns { return &Campaigns{db: db} }

// Create вставляет кампанию в статусе pending и возвращает её id.
func (cs *Campaigns) Create(ctx context.Context, clientID string, b campaign.Brief) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
	briefJSON, _ := json.Marshal(b)
	_, err := cs.db.ExecContext(ctx,
		`INSERT INTO campaigns (id, client_id, status, brief) VALUES (?, ?, 'pending', ?)`,
		id, clientID, string(briefJSON))
	return id, err
}

// MarkRunning переводит кампанию в running.

// MarkRunning переводит кампанию в running.
func (cs *Campaigns) MarkRunning(ctx context.Context, id string) error {
	_, err := cs.db.ExecContext(ctx,
		`UPDATE campaigns SET status='running', updated_at=`+nowExpr+` WHERE id=?`, id)
	return err
}

// SaveProgress сохраняет снимок прогресса прогона (перезаписывает прошлый).

// SaveProgress сохраняет снимок прогресса прогона (перезаписывает прошлый).
func (cs *Campaigns) SaveProgress(ctx context.Context, id string, snap run.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := cs.db.ExecContext(ctx,
		`UPDATE campaigns SET progress=?, updated_at=`+nowExpr+` WHERE id=?`, string(b), id)
	return err
}

// Complete сохраняет результат и переводит кампанию в done (вместе с deliverables).

// Complete сохраняет результат и переводит кампанию в done (вместе с deliverables).
func (cs *Campaigns) Complete(ctx context.Context, id string, res campaign.Outcome) error {
	stratJSON, _ := json.Marshal(res.Strategy)
	tx, err := cs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`UPDATE campaigns SET status='done', strategy=?, cost_usd=?, updated_at=`+nowExpr+` WHERE id=?`,
		string(stratJSON), res.CostUSD, id); err != nil {
		return err
	}
	for _, d := range res.Deliverables {
		reviewJSON, _ := json.Marshal(d.Review)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO deliverables (id, campaign_id, topic, title, body, cta, review)
			 VALUES (?,?,?,?,?,?,?)`,
			newUUID(), id, d.Topic, d.Title, d.Body, d.CTA, string(reviewJSON)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Fail переводит кампанию в failed с текстом ошибки.

// Fail переводит кампанию в failed с текстом ошибки.
func (cs *Campaigns) Fail(ctx context.Context, id, msg string) error {
	_, err := cs.db.ExecContext(ctx,
		`UPDATE campaigns SET status='failed', error=?, updated_at=`+nowExpr+` WHERE id=?`, msg, id)
	return err
}

// RecoverInterrupted помечает осиротевшие после рестарта кампании и проверки
// (pending/running) как failed. Возвращает общее число восстановленных. Идемпотентен.

// ListRecent возвращает до limit последних кампаний, новые сверху.
// rowid — тайбрейкер для записей с одинаковым created_at (точность — миллисекунды).
func (cs *Campaigns) ListRecent(ctx context.Context, limit int) ([]campaign.Summary, error) {
	rows, err := cs.db.QueryContext(ctx,
		`SELECT id, status, brief, cost_usd, created_at
		 FROM campaigns ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]campaign.Summary, 0, limit)
	for rows.Next() {
		var c campaign.Summary
		var briefJSON []byte
		var cost *float64
		if err := rows.Scan(&c.ID, &c.Status, &briefJSON, &cost, &c.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(briefJSON, &c.Brief)
		c.CostUSD = cost
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get читает кампанию вместе с deliverables.

// Get читает кампанию вместе с deliverables.
func (cs *Campaigns) Get(ctx context.Context, id string) (*campaign.Record, error) {
	var c campaign.Record
	var briefJSON, stratJSON, progressJSON []byte
	var cost *float64
	var errText *string
	err := cs.db.QueryRowContext(ctx,
		`SELECT id, client_id, status, brief, strategy, cost_usd, error, progress, created_at, updated_at
		 FROM campaigns WHERE id=?`, id).
		Scan(&c.ID, &c.ClientID, &c.Status, &briefJSON, &stratJSON, &cost, &errText, &progressJSON, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, campaign.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(briefJSON, &c.Brief)
	if len(stratJSON) > 0 {
		var st campaign.Strategy
		if json.Unmarshal(stratJSON, &st) == nil {
			c.Strategy = &st
		}
	}
	c.CostUSD = cost
	if errText != nil {
		c.Error = *errText
	}
	if len(progressJSON) > 0 {
		var snap run.Snapshot
		if json.Unmarshal(progressJSON, &snap) == nil {
			c.Progress = &snap
		}
	}

	rows, err := cs.db.QueryContext(ctx,
		`SELECT topic, title, body, cta, review FROM deliverables
		 WHERE campaign_id=? ORDER BY created_at, rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d campaign.Deliverable
		var reviewJSON []byte
		if err := rows.Scan(&d.Topic, &d.Title, &d.Body, &d.CTA, &reviewJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(reviewJSON, &d.Review)
		c.Deliverables = append(c.Deliverables, d)
	}
	return &c, rows.Err()
}
