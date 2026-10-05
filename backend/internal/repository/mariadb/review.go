package mariadb

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
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
type Reviews struct{ db *gorm.DB }

// NewReviews оборачивает соединение: сами запросы живут в этом файле.
func NewReviews(db *gorm.DB) *Reviews { return &Reviews{db: db} }

// CreateCheck вставляет проверку в статусе pending и возвращает её id.
func (rs *Reviews) CreateCheck(ctx context.Context, clientID, briefText string) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	id := newUUID()
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
	b, _ := json.Marshal(snap)
	return rs.update(ctx, id, map[string]any{"progress": string(b)})
}

// CompleteCheck сохраняет результат и переводит проверку в done.
func (rs *Reviews) CompleteCheck(ctx context.Context, id string, res review.Result) error {
	resultJSON, _ := json.Marshal(res)
	return rs.update(ctx, id, map[string]any{
		"status":   "done",
		"result":   string(resultJSON),
		"cost_usd": res.CostUSD,
	})
}

// FailCheck переводит проверку в failed с текстом ошибки.
func (rs *Reviews) FailCheck(ctx context.Context, id, msg string) error {
	return rs.update(ctx, id, map[string]any{"status": "failed", "error": msg})
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
	values["updated_at"] = nowUTC()
	return rs.db.WithContext(ctx).Model(&reviewRow{}).Where("id = ?", id).Updates(values).Error
}
