package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultClientID = "00000000-0000-0000-0000-000000000001"

var ErrNotFound = errors.New("campaign not found")

// Campaign — модель строки кампании для API.
type Campaign struct {
	ID           string                 `json:"id"`
	ClientID     string                 `json:"client_id"`
	Status       string                 `json:"status"`
	Brief        agents.Brief           `json:"brief"`
	Strategy     *agents.Strategy       `json:"strategy,omitempty"`
	Deliverables []agents.Deliverable   `json:"deliverables,omitempty"`
	Progress     *orchestrator.Snapshot `json:"progress,omitempty"`
	CostUSD      *float64               `json:"cost_usd,omitempty"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// CampaignSummary — лёгкая сводка для списка истории (без strategy/deliverables/body).
type CampaignSummary struct {
	ID        string       `json:"id"`
	Status    string       `json:"status"`
	Brief     agents.Brief `json:"brief"`
	CostUSD   *float64     `json:"cost_usd,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// newUUID генерирует UUID v4.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// Create вставляет кампанию в статусе pending и возвращает её id.
func (s *Store) Create(ctx context.Context, clientID string, b agents.Brief) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
	briefJSON, _ := json.Marshal(b)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO campaigns (id, client_id, status, brief) VALUES ($1, $2, 'pending', $3)`,
		id, clientID, briefJSON)
	return id, err
}

// MarkRunning переводит кампанию в running.
func (s *Store) MarkRunning(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status='running', updated_at=now() WHERE id=$1`, id)
	return err
}

// SaveProgress сохраняет снимок прогресса прогона (перезаписывает прошлый).
func (s *Store) SaveProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET progress=$2, updated_at=now() WHERE id=$1`, id, b)
	return err
}

// Complete сохраняет результат и переводит кампанию в done (вместе с deliverables).
func (s *Store) Complete(ctx context.Context, id string, res orchestrator.Result) error {
	stratJSON, _ := json.Marshal(res.Strategy)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`UPDATE campaigns SET status='done', strategy=$2, cost_usd=$3, updated_at=now() WHERE id=$1`,
		id, stratJSON, res.CostUSD); err != nil {
		return err
	}
	for _, d := range res.Deliverables {
		reviewJSON, _ := json.Marshal(d.Review)
		if _, err := tx.Exec(ctx,
			`INSERT INTO deliverables (id, campaign_id, topic, title, body, cta, review)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			newUUID(), id, d.Topic, d.Title, d.Body, d.CTA, reviewJSON); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Fail переводит кампанию в failed с текстом ошибки.
func (s *Store) Fail(ctx context.Context, id, msg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status='failed', error=$2, updated_at=now() WHERE id=$1`, id, msg)
	return err
}

// RecoverInterrupted помечает осиротевшие после рестарта кампании (pending/running)
// как failed. Возвращает число восстановленных. Идемпотентен.
func (s *Store) RecoverInterrupted(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status='failed', error='прервано рестартом сервиса', updated_at=now()
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListRecent возвращает до limit последних кампаний, новые сверху.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]CampaignSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, status, brief, cost_usd, created_at
		 FROM campaigns ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CampaignSummary, 0, limit)
	for rows.Next() {
		var c CampaignSummary
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
func (s *Store) Get(ctx context.Context, id string) (*Campaign, error) {
	var c Campaign
	var briefJSON, stratJSON, progressJSON []byte
	var cost *float64
	var errText *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, status, brief, strategy, cost_usd, error, progress, created_at, updated_at
		 FROM campaigns WHERE id=$1`, id).
		Scan(&c.ID, &c.ClientID, &c.Status, &briefJSON, &stratJSON, &cost, &errText, &progressJSON, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(briefJSON, &c.Brief)
	if len(stratJSON) > 0 {
		var st agents.Strategy
		if json.Unmarshal(stratJSON, &st) == nil {
			c.Strategy = &st
		}
	}
	c.CostUSD = cost
	if errText != nil {
		c.Error = *errText
	}
	if len(progressJSON) > 0 {
		var snap orchestrator.Snapshot
		if json.Unmarshal(progressJSON, &snap) == nil {
			c.Progress = &snap
		}
	}

	rows, err := s.pool.Query(ctx,
		`SELECT topic, title, body, cta, review FROM deliverables WHERE campaign_id=$1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d agents.Deliverable
		var reviewJSON []byte
		if err := rows.Scan(&d.Topic, &d.Title, &d.Body, &d.CTA, &reviewJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(reviewJSON, &d.Review)
		c.Deliverables = append(c.Deliverables, d)
	}
	return &c, rows.Err()
}
