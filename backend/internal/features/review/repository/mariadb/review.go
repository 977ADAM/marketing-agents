package mariadb

import (
	"context"
	"encoding/json"
	"errors"
	identity "github.com/977ADAM/marketing-agents/internal/core/identity"
	shared "github.com/977ADAM/marketing-agents/internal/core/repository/mariadb"
	"time"

	"gorm.io/gorm"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

// reviewRow — таблица reviews. Результат и прогресс — JSON текстом;
// created_at/updated_at только читаются (см. campaignRow).
type reviewRow struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ClientID  string    `gorm:"column:client_id;type:varchar(36);not null"`
	Status    string    `gorm:"column:status;type:varchar(32);not null"`
	BriefText string    `gorm:"column:brief_text;type:mediumtext;not null"`
	Result    *string   `gorm:"column:result;type:mediumtext"`
	Progress  *string   `gorm:"column:progress;type:mediumtext"`
	CostUSD   *float64  `gorm:"column:cost_usd"`
	Error     *string   `gorm:"column:error;type:mediumtext"`
	CreatedAt time.Time `gorm:"column:created_at;->"`
	UpdatedAt time.Time `gorm:"column:updated_at;->"`
}

func (reviewRow) TableName() string { return "reviews" }

// Reviews — хранилище reviews поверх общего соединения.
type Reviews struct {
	db  *gorm.DB
	now func() time.Time
}

// NewReviews оборачивает соединение: сами запросы живут в этом файле.
func NewReviews(db *gorm.DB, clocks ...func() time.Time) *Reviews {
	now := time.Now
	if len(clocks) > 0 {
		now = clocks[0]
	}
	return &Reviews{db: db, now: now}
}

// CreateCheck вставляет проверку в статусе pending и возвращает её id.
func (rs *Reviews) CreateCheck(ctx context.Context, clientID, briefText string) (string, error) {
	if clientID == "" {
		clientID = identity.DefaultClientID
	}
	id := identity.NewUUID()
	row := reviewRow{ID: id, ClientID: clientID, Status: "pending", BriefText: briefText}
	if err := rs.db.WithContext(ctx).Create(&row).Error; err != nil {
		return "", err
	}
	return id, nil
}

// MarkCheckRunning переводит проверку в running.
func (rs *Reviews) MarkCheckRunning(ctx context.Context, id string) error {
	return rs.update(ctx, id, map[string]any{"status": "running"})
}

// SaveCheckProgress сохраняет снимок прогресса проверки (перезаписывает прошлый).
func (rs *Reviews) SaveCheckProgress(ctx context.Context, id string, snap run.Snapshot) error {
	if snap.Revision <= 0 {
		snap.Revision = 1
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := shared.CheckFence(ctx, rs.db, "reviews", id, rs.now().UTC()); err != nil {
		return err
	}
	query := rs.db.WithContext(ctx).Model(&reviewRow{}).Where("id = ? AND progress_revision < ?", id, snap.Revision)
	query = shared.Fenced(ctx, query, rs.now().UTC())
	if snap.Phase == run.PhaseDone || snap.Phase == run.PhaseFailed {
		query = query.Where("status IN ? OR status = ?", []string{"pending", "running"}, string(snap.Phase))
	} else {
		query = query.Where("status IN ?", []string{"pending", "running"})
	}
	return query.Updates(map[string]any{"progress": string(b), "progress_revision": snap.Revision, "updated_at": time.Now().UTC()}).Error
}

// CompleteCheck сохраняет результат и переводит проверку в done.
func (rs *Reviews) CompleteCheck(ctx context.Context, id string, res review.Result) error {
	resultJSON, _ := json.Marshal(res)
	return rs.update(ctx, id, map[string]any{
		"status":   "done",
		"progress": gorm.Expr("JSON_SET(COALESCE(progress, '{}'), '$.phase', 'done', '$.percent', 100)"),
		"result":   string(resultJSON),
		"cost_usd": res.CostUSD,
	})
}

// FailCheck переводит проверку в failed с текстом ошибки.
func (rs *Reviews) FailCheck(ctx context.Context, id, msg string) error {
	return rs.update(ctx, id, map[string]any{"status": "failed", "progress": gorm.Expr("JSON_SET(COALESCE(progress, '{}'), '$.phase', 'failed')"), "error": msg})
}

// GetCheck читает проверку вместе с результатом.
func (rs *Reviews) GetCheck(ctx context.Context, id string) (*review.Record, error) {
	var row reviewRow
	err := rs.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, review.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r := review.Record{
		ID: row.ID, ClientID: row.ClientID, Status: row.Status, BriefText: row.BriefText,
		CostUSD: row.CostUSD, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.Result != nil && *row.Result != "" {
		var res review.Result
		if json.Unmarshal([]byte(*row.Result), &res) == nil {
			r.Result = &res
		}
	}
	if row.Error != nil {
		r.Error = *row.Error
	}
	if row.Progress != nil && *row.Progress != "" {
		var snap run.Snapshot
		if json.Unmarshal([]byte(*row.Progress), &snap) == nil {
			r.Progress = &snap
		}
	}
	found, err := shared.HasInput(ctx, rs.db, "review", id)
	if err != nil {
		return nil, err
	}
	r.ResumeAvailable = r.Status == "failed" && found
	rows, err := shared.CheckpointRows(ctx, rs.db, "review", id, "text")
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		result := review.Result{}
		for _, row := range rows {
			var d run.Saved[review.TextReport]
			if err := json.Unmarshal([]byte(row.Payload), &d); err != nil {
				return nil, err
			}
			result.Items = append(result.Items, d.Value)
			if d.Value.Verdict == review.VerdictPass {
				result.Passed++
			}
		}
		if r.Result != nil {
			result.CostUSD = r.Result.CostUSD
		}
		r.Result = &result
	}
	var summary run.RunSummary
	if ok, err := rs.LoadCheckpoint(ctx, id, "summary", 0, &summary); err != nil {
		return nil, err
	} else if ok {
		r.CostUSD = &summary.CostUSD
		if r.Result != nil {
			r.Result.CostUSD = summary.CostUSD
		}
	}
	return &r, nil
}

// ListChecks возвращает до limit последних проверок, новые сверху.
// seq — порядок вставки: тайбрейкер для записей с одинаковым created_at.
func (rs *Reviews) ListChecks(ctx context.Context, limit int) ([]review.Summary, error) {
	var rows []reviewRow
	if err := rs.db.WithContext(ctx).Order("created_at DESC, seq DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]review.Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, review.Summary{
			ID: r.ID, Status: r.Status, BriefText: r.BriefText,
			CostUSD: r.CostUSD, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// update — общий путь записи: updated_at ставим сами (в модели колонка только для
// чтения), в UTC.
func (rs *Reviews) update(ctx context.Context, id string, values map[string]any) error {
	values["updated_at"] = time.Now().UTC()
	if err := shared.CheckFence(ctx, rs.db, "reviews", id, rs.now().UTC()); err != nil {
		return err
	}
	return shared.Fenced(ctx, rs.db.WithContext(ctx).Model(&reviewRow{}).Where("id = ? AND status IN ?", id, []string{"pending", "running"}), rs.now().UTC()).Updates(values).Error
}

func (rs *Reviews) RecoverInterrupted(ctx context.Context) (int64, error) {
	return shared.RecoverExpired(ctx, rs.db, "reviews", rs.now().UTC(), 30*time.Second)
}

func (rs *Reviews) LookupCreation(ctx context.Context, client, key, hash string) (string, error) {
	return shared.LookupCreation(ctx, rs.db, client, "review", key, hash)
}
func (rs *Reviews) CreateOnce(ctx context.Context, client, key, hash string, req review.Request) (string, bool, error) {
	if client == "" {
		client = identity.DefaultClientID
	}
	return shared.CreateOnce(ctx, rs.db, client, "review", key, hash, func(tx *gorm.DB, id string) error {
		if err := tx.Create(&reviewRow{ID: id, ClientID: client, Status: "pending", BriefText: req.BriefText}).Error; err != nil {
			return err
		}
		return shared.SaveInitialCheckpoint(ctx, tx, "review", id, req)
	})
}

func (rs *Reviews) AcquireLease(ctx context.Context, id, owner string, ttl time.Duration) (int64, bool, error) {
	return shared.AcquireLease(ctx, rs.db, "reviews", id, owner, ttl, rs.now().UTC())
}
func (rs *Reviews) RenewLease(ctx context.Context, id, owner string, attempt int64, ttl time.Duration) error {
	return shared.RenewLease(ctx, rs.db, "reviews", id, owner, attempt, ttl, rs.now().UTC())
}

func (rs *Reviews) CreateReview(ctx context.Context, client string, req review.Request) (string, error) {
	if client == "" {
		client = identity.DefaultClientID
	}
	id := identity.NewUUID()
	err := rs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&reviewRow{ID: id, ClientID: client, Status: "pending", BriefText: req.BriefText}).Error; err != nil {
			return err
		}
		return shared.SaveInitialCheckpoint(ctx, tx, "review", id, req)
	})
	return id, err
}

func (rs *Reviews) LoadCheckpoint(ctx context.Context, id, stage string, pos int, out any) (bool, error) {
	return shared.LoadCheckpoint(ctx, rs.db, "review", id, stage, pos, out)
}
func (rs *Reviews) SaveCheckpoint(ctx context.Context, id, stage string, pos int, data any) error {
	return shared.SaveCheckpoint(ctx, rs.db, "reviews", "review", id, stage, pos, data, rs.now().UTC())
}
func (rs *Reviews) Requeue(ctx context.Context, id string) error {
	return shared.Requeue(ctx, rs.db, "reviews", "review", id, rs.now().UTC())
}
