package mariadb

import (
	"context"
	"encoding/json"
	"errors"
	identity "github.com/977ADAM/marketing-agents/internal/core/identity"
	llm "github.com/977ADAM/marketing-agents/internal/core/llm"
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
	Seq       int64     `gorm:"column:seq;->"`
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ClientID  string    `gorm:"column:client_id;type:varchar(36);not null"`
	Status    string    `gorm:"column:status;type:varchar(32);not null"`
	Brief     string    `gorm:"column:brief;type:mediumtext;not null"`
	Strategy  *string   `gorm:"column:strategy;type:mediumtext"`
	Progress  *string   `gorm:"column:progress;type:mediumtext"`
	CostKnown *bool     `gorm:"column:cost_known"`
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
type Campaigns struct {
	db  *gorm.DB
	now func() time.Time
}

// NewCampaigns оборачивает соединение: сами запросы живут в этом файле.
func NewCampaigns(db *gorm.DB, clocks ...func() time.Time) *Campaigns {
	now := time.Now
	if len(clocks) > 0 {
		now = clocks[0]
	}
	return &Campaigns{db: db, now: now}
}

// Create вставляет кампанию в статусе pending и возвращает её id.
func (cs *Campaigns) Create(ctx context.Context, clientID string, b campaign.Brief) (string, error) {
	if clientID == "" {
		clientID = identity.DefaultClientID
	}
	id := identity.NewUUID()
	briefJSON, _ := json.Marshal(b)
	row := campaignRow{ID: id, ClientID: clientID, Status: "pending", Brief: string(briefJSON)}
	if err := cs.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return shared.SaveInitialCheckpoint(ctx, tx, "campaign", id, b)
	}); err != nil {
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
	if err := shared.CheckFence(ctx, cs.db, "campaigns", id, cs.now().UTC()); err != nil {
		return err
	}
	query := cs.db.WithContext(ctx).Model(&campaignRow{}).Where("id = ? AND progress_revision < ?", id, snap.Revision)
	query = shared.Fenced(ctx, query, cs.now().UTC())
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
		if err := shared.CheckFence(ctx, tx, "campaigns", id, cs.now().UTC()); err != nil {
			return err
		}
		if err := shared.Fenced(ctx, tx.Model(&campaignRow{}).Where("id = ? AND status IN ?", id, []string{"pending", "running"}), cs.now().UTC()).Updates(values).Error; err != nil {
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
	rows, _, err := cs.ListRecentPage(ctx, limit, 0)
	return rows, err
}
func (cs *Campaigns) ListRecentPage(ctx context.Context, limit int, before int64) ([]campaign.Summary, int64, error) {
	var rows []campaignRow
	q := cs.db.WithContext(ctx).Order("seq DESC").Limit(limit + 1)
	if before > 0 {
		q = q.Where("seq < ?", before)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	var next int64
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Seq
	}
	out := make([]campaign.Summary, 0, len(rows))
	for _, r := range rows {
		s := campaign.Summary{ID: r.ID, Status: r.Status, CostKnown: r.CostKnown, CostUSD: r.CostUSD, CreatedAt: r.CreatedAt}
		if err := json.Unmarshal([]byte(r.Brief), &s.Brief); err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, next, nil
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
		CostKnown: row.CostKnown, CostUSD: row.CostUSD, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(row.Brief), &c.Brief); err != nil {
		return nil, err
	}
	if row.Strategy != nil && *row.Strategy != "" {
		var st campaign.Strategy
		if err := json.Unmarshal([]byte(*row.Strategy), &st); err != nil {
			return nil, err
		}
		c.Strategy = &st
	}
	if row.Error != nil {
		c.Error = *row.Error
	}
	if row.Progress != nil && *row.Progress != "" {
		var snap run.Snapshot
		if err := json.Unmarshal([]byte(*row.Progress), &snap); err != nil {
			return nil, err
		}
		c.Progress = &snap
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
		if err := json.Unmarshal([]byte(d.Review), &del.Review); err != nil {
			return nil, err
		}
		c.Deliverables = append(c.Deliverables, del)
	}
	found, err := shared.HasInput(ctx, cs.db, "campaign", id)
	if err != nil {
		return nil, err
	}
	c.ResumeAvailable = c.Status == "failed" && found
	var saved run.Saved[campaign.Strategy]
	if ok, err := cs.LoadCheckpoint(ctx, id, "strategy", 0, &saved); err != nil {
		return nil, err
	} else if ok && c.Strategy == nil {
		c.Strategy = &saved.Value
	}
	rows, err := shared.CheckpointRows(ctx, cs.db, "campaign", id, "article")
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		c.Deliverables = nil
		for _, row := range rows {
			var d run.Saved[campaign.Deliverable]
			if err := json.Unmarshal([]byte(row.Payload), &d); err != nil {
				return nil, err
			}
			c.Deliverables = append(c.Deliverables, d.Value)
		}
	}
	var summary run.RunSummary
	if ok, err := cs.LoadCheckpoint(ctx, id, "summary", 0, &summary); err != nil {
		return nil, err
	} else if ok {
		c.CostUSD = &summary.CostUSD
		c.CostKnown = &summary.CostKnown
		c.Usage = &summary.Usage
	}
	return &c, nil
}

// update — общий путь записи: updated_at ставим сами (в модели колонка только для
// чтения), в UTC.
func (cs *Campaigns) update(ctx context.Context, id string, values map[string]any) error {
	values["updated_at"] = time.Now().UTC()
	if err := shared.CheckFence(ctx, cs.db, "campaigns", id, cs.now().UTC()); err != nil {
		return err
	}
	return shared.Fenced(ctx, cs.db.WithContext(ctx).Model(&campaignRow{}).Where("id = ? AND status IN ?", id, []string{"pending", "running"}), cs.now().UTC()).Updates(values).Error
}

func (rs *Campaigns) RecoverInterrupted(ctx context.Context) (int64, error) {
	return shared.RecoverExpired(ctx, rs.db, "campaigns", rs.now().UTC(), 30*time.Second)
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
		if err := tx.Create(&campaignRow{ID: id, ClientID: client, Status: "pending", Brief: string(data)}).Error; err != nil {
			return err
		}
		return shared.SaveInitialCheckpoint(ctx, tx, "campaign", id, b)
	})
}

func (cs *Campaigns) AcquireLease(ctx context.Context, id, owner string, ttl time.Duration) (int64, bool, error) {
	return shared.AcquireLease(ctx, cs.db, "campaigns", id, owner, ttl, cs.now().UTC())
}
func (cs *Campaigns) RenewLease(ctx context.Context, id, owner string, attempt int64, ttl time.Duration) error {
	return shared.RenewLease(ctx, cs.db, "campaigns", id, owner, attempt, ttl, cs.now().UTC())
}

func (cs *Campaigns) LoadCheckpoint(ctx context.Context, id, stage string, pos int, out any) (bool, error) {
	return shared.LoadCheckpoint(ctx, cs.db, "campaign", id, stage, pos, out)
}
func (cs *Campaigns) SaveCheckpoint(ctx context.Context, id, stage string, pos int, data any) error {
	return shared.SaveCheckpoint(ctx, cs.db, "campaigns", "campaign", id, stage, pos, data, cs.now().UTC())
}
func (cs *Campaigns) Requeue(ctx context.Context, id string) error {
	return shared.Requeue(ctx, cs.db, "campaigns", "campaign", id, cs.now().UTC())
}

func (cs *Campaigns) AppendUsage(ctx context.Context, id string, e llm.UsageEntry) error {
	return shared.AppendUsage(ctx, cs.db, "campaigns", "campaign", id, e, cs.now().UTC())
}
func (cs *Campaigns) UsageEntries(ctx context.Context, id string) ([]llm.UsageEntry, error) {
	return shared.UsageEntries(ctx, cs.db, "campaign", id)
}
