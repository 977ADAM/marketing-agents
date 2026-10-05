// Package store — персистентность кампаний и проверок текстов на SQLite.
//
// Драйвер modernc.org/sqlite — чистый Go (без CGO), поэтому бинарь остаётся
// статическим: сборка идёт с CGO_ENABLED=0, рантайм — distroless.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // регистрирует драйвер "sqlite"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/run"
)

const DefaultClientID = "00000000-0000-0000-0000-000000000001"

// nowExpr — SQL-выражение «сейчас» в формате, который драйвер разбирает в
// time.Time (колонки объявлены как DATETIME): 'YYYY-MM-DD HH:MM:SS.mmm', UTC.
const nowExpr = `strftime('%Y-%m-%d %H:%M:%f','now')`

// pragmas — настройки соединения: ждать снятия блокировки при конкурентной
// записи, WAL (читатели не блокируют писателя), внешние ключи, меньше fsync.
// _txlock=immediate — транзакция на запись берёт блокировку сразу, поэтому
// работает busy_timeout, а не падает с SQLITE_BUSY_SNAPSHOT.
const pragmas = "_txlock=immediate&_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

var ErrNotFound = errors.New("not found")

// DSN собирает URI подключения к файлу БД с нужными pragma.
func DSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		return path // вызывающая сторона передала готовый URI
	}
	if path == ":memory:" {
		// WAL для in-memory БД бессмысленен (SQLite оставляет journal_mode=memory).
		return "file::memory:?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	return "file:" + path + "?" + pragmas
}

// Store — доступ к БД. Реализует интерфейсы httpapi.Repo и httpapi.ProgressStore.
type Store struct{ db *sql.DB }

// New оборачивает уже открытое соединение (без миграций).
func New(db *sql.DB) *Store { return &Store{db: db} }

// OpenDB открывает (при необходимости создаёт каталог и файл) соединение с БД.
// Миграции не применяет: это отдельный шаг — сервис migrate в docker-compose или
// `make migrate` (см. internal/migrate), а сервер до старта проверяет готовность
// схемы через migrate.CheckReady.
func OpenDB(ctx context.Context, path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: empty sqlite path")
	}
	if err := ensureDir(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	return db, nil
}

// Open открывает БД и возвращает готовый Store. Схему не мигрирует: считается,
// что миграции применены отдельным сервисом (см. internal/migrate).
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := OpenDB(ctx, path)
	if err != nil {
		return nil, err
	}
	return New(db), nil
}

// Close закрывает соединение с БД.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// ensureDir создаёт каталог под файл БД, если его нет.
func ensureDir(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: create dir %s: %w", dir, err)
	}
	return nil
}

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

// Campaign — модель строки кампании для API.
type Campaign struct {
	ID           string                 `json:"id"`
	ClientID     string                 `json:"client_id"`
	Status       string                 `json:"status"`
	Brief        campaign.Brief         `json:"brief"`
	Strategy     *campaign.Strategy     `json:"strategy,omitempty"`
	Deliverables []campaign.Deliverable `json:"deliverables,omitempty"`
	Progress     *run.Snapshot          `json:"progress,omitempty"`
	CostUSD      *float64               `json:"cost_usd,omitempty"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// CampaignSummary — лёгкая сводка для списка истории (без strategy/deliverables/body).
type CampaignSummary struct {
	ID        string         `json:"id"`
	Status    string         `json:"status"`
	Brief     campaign.Brief `json:"brief"`
	CostUSD   *float64       `json:"cost_usd,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Create вставляет кампанию в статусе pending и возвращает её id.
func (s *Store) Create(ctx context.Context, clientID string, b campaign.Brief) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
	briefJSON, _ := json.Marshal(b)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO campaigns (id, client_id, status, brief) VALUES (?, ?, 'pending', ?)`,
		id, clientID, string(briefJSON))
	return id, err
}

// MarkRunning переводит кампанию в running.
func (s *Store) MarkRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE campaigns SET status='running', updated_at=`+nowExpr+` WHERE id=?`, id)
	return err
}

// SaveProgress сохраняет снимок прогресса прогона (перезаписывает прошлый).
func (s *Store) SaveProgress(ctx context.Context, id string, snap run.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := s.db.ExecContext(ctx,
		`UPDATE campaigns SET progress=?, updated_at=`+nowExpr+` WHERE id=?`, string(b), id)
	return err
}

// Complete сохраняет результат и переводит кампанию в done (вместе с deliverables).
func (s *Store) Complete(ctx context.Context, id string, res orchestrator.Result) error {
	stratJSON, _ := json.Marshal(res.Strategy)
	tx, err := s.db.BeginTx(ctx, nil)
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
func (s *Store) Fail(ctx context.Context, id, msg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE campaigns SET status='failed', error=?, updated_at=`+nowExpr+` WHERE id=?`, msg, id)
	return err
}

// RecoverInterrupted помечает осиротевшие после рестарта кампании и проверки
// (pending/running) как failed. Возвращает общее число восстановленных. Идемпотентен.
func (s *Store) RecoverInterrupted(ctx context.Context) (int64, error) {
	tag, err := s.db.ExecContext(ctx,
		`UPDATE campaigns SET status='failed', error='прервано рестартом сервиса', updated_at=`+nowExpr+`
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, err
	}
	n, err := tag.RowsAffected()
	if err != nil {
		return 0, err
	}
	tag, err = s.db.ExecContext(ctx,
		`UPDATE reviews SET status='failed', error='прервано рестартом сервиса', updated_at=`+nowExpr+`
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, err
	}
	m, err := tag.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n + m, nil
}

// ListRecent возвращает до limit последних кампаний, новые сверху.
// rowid — тайбрейкер для записей с одинаковым created_at (точность — миллисекунды).
func (s *Store) ListRecent(ctx context.Context, limit int) ([]CampaignSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, brief, cost_usd, created_at
		 FROM campaigns ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
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
	err := s.db.QueryRowContext(ctx,
		`SELECT id, client_id, status, brief, strategy, cost_usd, error, progress, created_at, updated_at
		 FROM campaigns WHERE id=?`, id).
		Scan(&c.ID, &c.ClientID, &c.Status, &briefJSON, &stratJSON, &cost, &errText, &progressJSON, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
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

	rows, err := s.db.QueryContext(ctx,
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
