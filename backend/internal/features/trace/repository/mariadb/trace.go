package mariadb

import (
	"context"
	"errors"
	identity "github.com/977ADAM/marketing-agents/internal/core/identity"
	"time"

	"gorm.io/gorm"

	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// runEventRow — таблица run_events (трасса прогона). payload заполняется только в
// режиме full, поэтому колонка nullable; summary есть всегда.
type runEventRow struct {
	ID               string    `gorm:"column:id;type:varchar(36);primaryKey"`
	RunID            string    `gorm:"column:run_id;type:varchar(64);not null"`
	Seq              int64     `gorm:"column:seq;not null"`
	At               time.Time `gorm:"column:at;not null"`
	Kind             string    `gorm:"column:kind;type:varchar(32);not null"`
	Name             string    `gorm:"column:name;type:varchar(64);not null"`
	Status           string    `gorm:"column:status;type:varchar(32);not null"`
	DurationMS       int64     `gorm:"column:duration_ms;not null"`
	PromptTokens     int       `gorm:"column:prompt_tokens;not null"`
	CompletionTokens int       `gorm:"column:completion_tokens;not null"`
	Summary          string    `gorm:"column:summary;type:mediumtext;not null"`
	Payload          *string   `gorm:"column:payload;type:mediumtext"`
	Error            *string   `gorm:"column:error;type:mediumtext"`
}

func (runEventRow) TableName() string { return "run_events" }

// Events — хранилище events поверх общего соединения.
type Events struct{ db *gorm.DB }

// NewEvents оборачивает соединение: сами запросы живут в этом файле.
func NewEvents(db *gorm.DB) *Events { return &Events{db: db} }

// SaveRunEvent сохраняет событие трассы (реализует trace.Sink).
func (es *Events) SaveRunEvent(ctx context.Context, rec trace.Record) error {
	row := runEventRow{
		ID: identity.NewUUID(), RunID: rec.RunID, Seq: rec.Seq, At: rec.At.UTC(),
		Kind: string(rec.Kind), Name: rec.Name, Status: string(rec.Status),
		DurationMS: rec.DurationMS, PromptTokens: rec.PromptTokens,
		CompletionTokens: rec.CompletionTokens, Summary: rec.Summary,
		Payload: nullable(rec.PayloadJSON), Error: nullable(rec.Error),
	}
	return es.db.WithContext(ctx).Create(&row).Error
}

// RunEvents возвращает ленту событий прогона без тел payload: их отдают отдельным
// запросом, иначе ответ разрастается до мегабайт.
func (es *Events) RunEvents(ctx context.Context, runID string, limit int) ([]trace.Row, error) {
	return es.eventsAfter(ctx, runID, 0, limit)
}
func (es *Events) eventsAfter(ctx context.Context, runID string, after int64, limit int) ([]trace.Row, error) {
	if limit <= 0 {
		limit = 500
	}
	// has_payload считает БД: в ленте тело не нужно, но признак «тело есть» нужен
	// интерфейсу (у самого события поле nullable).
	var rows []struct {
		Seq              int64
		At               time.Time
		Kind             string
		Name             string
		Status           string
		DurationMS       int64
		PromptTokens     int
		CompletionTokens int
		Summary          string
		HasPayload       int
		Error            string
	}
	err := es.db.WithContext(ctx).Model(&runEventRow{}).
		Select(`seq, at, kind, name, status, duration_ms, prompt_tokens,
		        completion_tokens, summary,
		        (payload IS NOT NULL AND payload <> '') AS has_payload,
		        IFNULL(error, '') AS error`).
		Where("run_id = ? AND seq > ?", runID, after).Order("seq").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]trace.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, trace.Row{
			Seq: r.Seq, At: r.At, Kind: r.Kind, Name: r.Name, Status: r.Status,
			DurationMS: r.DurationMS, PromptTokens: r.PromptTokens,
			CompletionTokens: r.CompletionTokens, Summary: r.Summary,
			HasPayload: r.HasPayload == 1, Error: r.Error,
		})
	}
	return out, nil
}

// RunEvent возвращает одно событие прогона вместе с payload.
func (es *Events) RunEvent(ctx context.Context, runID string, seq int64) (*trace.Row, error) {
	var row runEventRow
	err := es.db.WithContext(ctx).Where("run_id = ? AND seq = ?", runID, seq).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, trace.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ev := trace.Row{
		Seq: row.Seq, At: row.At, Kind: row.Kind, Name: row.Name, Status: row.Status,
		DurationMS: row.DurationMS, PromptTokens: row.PromptTokens,
		CompletionTokens: row.CompletionTokens, Summary: row.Summary,
	}
	if row.Payload != nil {
		ev.Payload = *row.Payload
		ev.HasPayload = *row.Payload != ""
	}
	if row.Error != nil {
		ev.Error = *row.Error
	}
	return &ev, nil
}

// DeleteRunEventsBefore удаляет события старше cutoff (ретенция). Возвращает
// число удалённых строк; сами прогоны не трогает.
func (es *Events) DeleteRunEventsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := es.db.WithContext(ctx).Where("at < ?", cutoff.UTC()).Delete(&runEventRow{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// nullable превращает пустую строку в NULL: так в БД видно «поля нет», а не
// «поле пустое».
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (es *Events) SaveSequencedEvent(ctx context.Context, rec trace.Record) error {
	return es.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO run_event_sequences (run_id,last_seq) VALUES (?,0) ON DUPLICATE KEY UPDATE last_seq=last_seq", rec.RunID).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE run_event_sequences SET last_seq=GREATEST(last_seq,(SELECT IFNULL(MAX(seq),0) FROM run_events WHERE run_id=?))+1 WHERE run_id=?", rec.RunID, rec.RunID).Error; err != nil {
			return err
		}
		if err := tx.Table("run_event_sequences").Select("last_seq").Where("run_id=?", rec.RunID).Scan(&rec.Seq).Error; err != nil {
			return err
		}
		return NewEvents(tx).SaveRunEvent(ctx, rec)
	})
}

func (es *Events) RunEventsPage(ctx context.Context, id string, after int64, limit int) (trace.Page, error) {
	if limit <= 0 {
		limit = 500
	}
	var total int64
	if err := es.db.WithContext(ctx).Model(&runEventRow{}).Where("run_id=?", id).Count(&total).Error; err != nil {
		return trace.Page{}, err
	}
	rows, err := es.eventsAfter(ctx, id, after, limit+1)
	if err != nil {
		return trace.Page{}, err
	}
	page := trace.Page{Rows: rows, Total: int(total), HasMore: len(rows) > limit}
	if page.HasMore {
		page.Rows = rows[:limit]
	}
	if len(page.Rows) > 0 {
		page.NextSeq = page.Rows[len(page.Rows)-1].Seq
	}
	return page, nil
}
