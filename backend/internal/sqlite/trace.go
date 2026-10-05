package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/977ADAM/marketing-agents/internal/trace"
)

// Events — хранилище events поверх общего соединения.
type Events struct{ db *sql.DB }

// NewEvents оборачивает соединение: сам SQL живёт в этом файле.
func NewEvents(db *sql.DB) *Events { return &Events{db: db} }

// timeLayout — формат, который драйвер modernc.org/sqlite разбирает в time.Time.
// Совпадает с тем, что даёт strftime('%Y-%m-%d %H:%M:%f','now') в схеме.
const timeLayout = "2006-01-02 15:04:05.000"

// SaveRunEvent сохраняет событие трассы (реализует trace.Sink).
func (es *Events) SaveRunEvent(ctx context.Context, rec trace.Record) error {
	_, err := es.db.ExecContext(ctx,
		`INSERT INTO run_events
			(id, run_id, seq, at, kind, name, status, duration_ms,
			 prompt_tokens, completion_tokens, summary, payload, error)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		newUUID(), rec.RunID, rec.Seq, rec.At.UTC().Format(timeLayout),
		string(rec.Kind), rec.Name, string(rec.Status), rec.DurationMS,
		rec.PromptTokens, rec.CompletionTokens, rec.Summary,
		nullable(rec.PayloadJSON), nullable(rec.Error))
	return err
}

// RunEvents возвращает ленту событий прогона без тел payload: их отдают отдельным
// запросом, иначе ответ разрастается до мегабайт.
func (es *Events) RunEvents(ctx context.Context, runID string, limit int) ([]trace.Row, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := es.db.QueryContext(ctx,
		`SELECT seq, at, kind, name, status, duration_ms, prompt_tokens, completion_tokens,
		        summary, payload IS NOT NULL AND payload <> '', IFNULL(error,'')
		 FROM run_events WHERE run_id=? ORDER BY seq LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]trace.Row, 0, limit)
	for rows.Next() {
		var ev trace.Row
		if err := rows.Scan(&ev.Seq, &ev.At, &ev.Kind, &ev.Name, &ev.Status, &ev.DurationMS,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.Summary, &ev.HasPayload, &ev.Error); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// RunEvent возвращает одно событие прогона вместе с payload.
func (es *Events) RunEvent(ctx context.Context, runID string, seq int64) (*trace.Row, error) {
	var ev trace.Row
	var payload, errText sql.NullString
	err := es.db.QueryRowContext(ctx,
		`SELECT seq, at, kind, name, status, duration_ms, prompt_tokens, completion_tokens,
		        summary, payload, error
		 FROM run_events WHERE run_id=? AND seq=?`, runID, seq).
		Scan(&ev.Seq, &ev.At, &ev.Kind, &ev.Name, &ev.Status, &ev.DurationMS,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.Summary, &payload, &errText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, trace.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ev.Payload = payload.String
	ev.HasPayload = payload.Valid && payload.String != ""
	ev.Error = errText.String
	return &ev, nil
}

// DeleteRunEventsBefore удаляет события старше cutoff (ретенция). Возвращает
// число удалённых строк; сами прогоны не трогает.
func (es *Events) DeleteRunEventsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := es.db.ExecContext(ctx,
		`DELETE FROM run_events WHERE at < ?`, cutoff.UTC().Format(timeLayout))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected()
}

// nullable превращает пустую строку в NULL: так в БД видно «поля нет», а не
// «поле пустое».
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
