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
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
)

// campaignRow — таблица campaigns. JSON (бриф, стратегия, прогресс) лежит текстом:
// разбирает и собирает его Go. created_at/updated_at только читаются (->):
// created_at ставит БД при вставке, updated_at обновляем явно вместе с данными.
type campaignRow struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ClientID  string    `gorm:"column:client_id;type:varchar(36);not null"`
	Status    string    `gorm:"column:status;type:varchar(32);not null"`
	Brief     string    `gorm:"column:brief;type:mediumtext;not null"`
	Strategy  *string   `gorm:"column:strategy;type:mediumtext"`
	Progress  *string   `gorm:"column:progress;type:mediumtext"`
	CostUSD   *float64  `gorm:"column:cost_usd"`
	Error     *string   `gorm:"column:error;type:mediumtext"`
	CreatedAt time.Time `gorm:"column:created_at;->"`
	UpdatedAt time.Time `gorm:"column:updated_at;->"`
}

func (campaignRow) TableName() string { return "campaigns" }

// deliverableRow — таблица deliverables: статья с ревью. position задаёт порядок
// статей в медиаплане (created_at у них общий — вставка одной транзакцией).
type deliverableRow struct {
	ID         string    `gorm:"column:id;type:varchar(36);primaryKey"`
	CampaignID string    `gorm:"column:campaign_id;type:varchar(36);not null"`
	Position   int       `gorm:"column:position;not null"`
	Topic      string    `gorm:"column:topic;type:text;not null"`
	Title      string    `gorm:"column:title;type:text;not null"`
	Body       string    `gorm:"column:body;type:mediumtext;not null"`
	CTA        string    `gorm:"column:cta;type:text;not null"`
	Review     string    `gorm:"column:review;type:mediumtext;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;->"`
}

func (deliverableRow) TableName() string { return "deliverables" }

// Campaigns — хранилище campaigns поверх общего соединения.
type Campaigns struct{ db *gorm.DB }

// NewCampaigns оборачивает соединение: сами запросы живут в этом файле.
func NewCampaigns(db *gorm.DB) *Campaigns { return &Campaigns{db: db} }

// Create вставляет кампанию в статусе pending и возвращает её id.
func (cs *Campaigns) Create(ctx context.Context, clientID string, b campaign.Brief) (string, error) {
	if clientID == "" {
		clientID = identity.DefaultClientID
	}
	id := identity.NewUUID()
	briefJSON, _ := json.Marshal(b)
	row := campaignRow{ID: id, ClientID: clientID, Status: "pending", Brief: string(briefJSON)}
	if err := cs.db.WithContext(ctx).Create(&row).Error; err != nil {
		return "", err
	}
	return id, nil
}

// MarkRunning переводит кампанию в running.
func (cs *Campaigns) MarkRunning(ctx context.Context, id string) error {
	return cs.update(ctx, id, map[string]any{"status": "running"})
}

// SaveProgress сохраняет снимок прогресса прогона (перезаписывает прошлый).
func (cs *Campaigns) SaveProgress(ctx context.Context, id string, snap run.Snapshot) error {
	if snap.Revision <= 0 {
		snap.Revision = 1
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	query := cs.db.WithContext(ctx).Model(&campaignRow{}).Where("id = ? AND progress_revision < ?", id, snap.Revision)
	if snap.Phase == run.PhaseDone || snap.Phase == run.PhaseFailed {
		query = query.Where("status IN ? OR status = ?", []string{"pending", "running"}, string(snap.Phase))
	} else {
		query = query.Where("status IN ?", []string{"pending", "running"})
	}
	return query.Updates(map[string]any{"progress": string(b), "progress_revision": snap.Revision, "updated_at": time.Now().UTC()}).Error
}

// Fail переводит кампанию в failed с текстом ошибки.
func (cs *Campaigns) Fail(ctx context.Context, id, msg string) error {
	return cs.update(ctx, id, map[string]any{"status": "failed", "progress": gorm.Expr("JSON_SET(COALESCE(progress, '{}'), '$.phase', 'failed')"), "error": msg})
}

// Complete сохраняет результат и переводит кампанию в done (вместе с deliverables).
// Транзакция своя: либо результат со статьями целиком, либо ничего.
func (cs *Campaigns) Complete(ctx context.Context, id string, res campaign.Outcome) error {
	stratJSON, _ := json.Marshal(res.Strategy)
	return cs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{
			"status":     "done",
			"progress":   gorm.Expr("JSON_SET(COALESCE(progress, '{}'), '$.phase', 'done', '$.percent', 100)"),
			"strategy":   string(stratJSON),
			"cost_usd":   res.CostUSD,
			"updated_at": time.Now().UTC(),
		}
		if err := tx.Model(&campaignRow{}).Where("id = ?", id).Updates(values).Error; err != nil {
			return err
		}
		if len(res.Deliverables) == 0 {
			return nil
		}
		rows := make([]deliverableRow, 0, len(res.Deliverables))
		for i, d := range res.Deliverables {
			reviewJSON, _ := json.Marshal(d.Review)
			rows = append(rows, deliverableRow{
				ID:         identity.NewUUID(),
				CampaignID: id,
				Position:   i,
				Topic:      d.Topic,
				Title:      d.Title,
				Body:       d.Body,
				CTA:        d.CTA,
				Review:     string(reviewJSON),
			})
		}
		return tx.Create(&rows).Error
	})
}

// ListRecent возвращает до limit последних кампаний, новые сверху.
// seq — порядок вставки: тайбрейкер для записей с одинаковым created_at
// (точность — миллисекунды).
func (cs *Campaigns) ListRecent(ctx context.Context, limit int) ([]campaign.Summary, error) {
	var rows []campaignRow
	if err := cs.db.WithContext(ctx).Order("created_at DESC, seq DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]campaign.Summary, 0, len(rows))
	for _, r := range rows {
		s := campaign.Summary{ID: r.ID, Status: r.Status, CostUSD: r.CostUSD, CreatedAt: r.CreatedAt}
		_ = json.Unmarshal([]byte(r.Brief), &s.Brief)
		out = append(out, s)
	}
	return out, nil
}

// Get читает кампанию вместе с deliverables.
func (cs *Campaigns) Get(ctx context.Context, id string) (*campaign.Record, error) {
	var row campaignRow
	err := cs.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, campaign.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c := campaign.Record{
		ID: row.ID, ClientID: row.ClientID, Status: row.Status,
		CostUSD: row.CostUSD, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	_ = json.Unmarshal([]byte(row.Brief), &c.Brief)
	if row.Strategy != nil && *row.Strategy != "" {
		var st campaign.Strategy
		if json.Unmarshal([]byte(*row.Strategy), &st) == nil {
			c.Strategy = &st
		}
	}
	if row.Error != nil {
		c.Error = *row.Error
	}
	if row.Progress != nil && *row.Progress != "" {
		var snap run.Snapshot
		if json.Unmarshal([]byte(*row.Progress), &snap) == nil {
			c.Progress = &snap
		}
	}

	// Порядок статей — как в медиаплане (position), а не по времени.
	var drows []deliverableRow
	if err := cs.db.WithContext(ctx).Where("campaign_id = ?", id).
		Order("position, created_at").Find(&drows).Error; err != nil {
		return nil, err
	}
	for _, d := range drows {
		del := campaign.Deliverable{Article: campaign.Article{
			Topic: d.Topic, Title: d.Title, Body: d.Body, CTA: d.CTA,
		}}
		_ = json.Unmarshal([]byte(d.Review), &del.Review)
		c.Deliverables = append(c.Deliverables, del)
	}
	return &c, nil
}

// update — общий путь записи: updated_at ставим сами (в модели колонка только для
// чтения), в UTC.
func (cs *Campaigns) update(ctx context.Context, id string, values map[string]any) error {
	values["updated_at"] = time.Now().UTC()
	return cs.db.WithContext(ctx).Model(&campaignRow{}).Where("id = ?", id).Updates(values).Error
}

func (rs *Campaigns) RecoverInterrupted(ctx context.Context) (int64, error) {
	res := rs.db.WithContext(ctx).Model(&campaignRow{}).Where("status IN ?", []string{"pending", "running"}).Updates(map[string]any{"status": "failed", "error": "прервано рестартом сервиса", "updated_at": time.Now().UTC()})
	return res.RowsAffected, res.Error
}

func (cs *Campaigns) LookupCreation(ctx context.Context, client, key, hash string) (string, error) {
	return shared.LookupCreation(ctx, cs.db, client, "campaign", key, hash)
}
func (cs *Campaigns) CreateOnce(ctx context.Context, client, key, hash string, b campaign.Brief) (string, bool, error) {
	if client == "" {
		client = identity.DefaultClientID
	}
	data, _ := json.Marshal(b)
	return shared.CreateOnce(ctx, cs.db, client, "campaign", key, hash, func(tx *gorm.DB, id string) error {
		return tx.Create(&campaignRow{ID: id, ClientID: client, Status: "pending", Brief: string(data)}).Error
	})
}
