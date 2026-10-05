package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/trace"
)

// --- API: трасса прогона ---

// Лимиты ленты: одно событие — сотни байт, но прогон с длинным циклом критика
// может дать их десятки, поэтому limit ограничен и сверху.
const (
	defaultTrajectoryLimit = 500
	maxTrajectoryLimit     = 2000
)

// trajectoryEvent — событие в ленте, без тела payload: тела отдаются отдельным
// запросом, иначе ответ разрастается.
type trajectoryEvent struct {
	Seq              int64     `json:"seq"`
	At               time.Time `json:"at"`
	Kind             string    `json:"kind"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	Summary          string    `json:"summary"`
	DurationMS       int64     `json:"duration_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	HasPayload       bool      `json:"has_payload"`
	Error            string    `json:"error,omitempty"`
}

// trajectoryEventFull — событие с телом (в режиме full там промпт и ответ).
type trajectoryEventFull struct {
	trajectoryEvent
	Payload json.RawMessage `json:"payload,omitempty"`
}

type trajectoryResponse struct {
	ID     string            `json:"id"`
	Total  int               `json:"total"`
	Events []trajectoryEvent `json:"events"`
}

func (a *API) campaignTrajectory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectory(w, r, id, func(ctx context.Context) error {
		_, err := a.campaigns.Get(ctx, id)
		return err
	})
}

func (a *API) campaignTrajectoryEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectoryEvent(w, r, id, func(ctx context.Context) error {
		_, err := a.campaigns.Get(ctx, id)
		return err
	})
}

func (a *API) reviewTrajectory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectory(w, r, id, func(ctx context.Context) error {
		_, err := a.reviews.GetReview(ctx, id)
		return err
	})
}

func (a *API) reviewTrajectoryEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectoryEvent(w, r, id, func(ctx context.Context) error {
		_, err := a.reviews.GetReview(ctx, id)
		return err
	})
}

// writeTrajectory отдаёт ленту событий прогона. exists проверяет, что прогон есть:
// иначе трасса несуществующей кампании выглядела бы как «пустая».
func (a *API) writeTrajectory(w http.ResponseWriter, r *http.Request, id string, exists func(context.Context) error) {
	if err := exists(r.Context()); err != nil {
		a.writeRunLookupError(w, err, "campaign")
		return
	}

	limit := defaultTrajectoryLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxTrajectoryLimit {
		limit = maxTrajectoryLimit
	}

	rows, err := a.traces.RunEvents(r.Context(), id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load trajectory")
		return
	}

	events := make([]trajectoryEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, trajectoryEvent{
			Seq: row.Seq, At: row.At, Kind: row.Kind, Name: row.Name, Status: row.Status,
			Summary: row.Summary, DurationMS: row.DurationMS, PromptTokens: row.PromptTokens,
			CompletionTokens: row.CompletionTokens, HasPayload: row.HasPayload, Error: row.Error,
		})
	}
	writeJSON(w, http.StatusOK, trajectoryResponse{ID: id, Total: len(events), Events: events})
}

// writeTrajectoryEvent отдаёт одно событие вместе с телом.
func (a *API) writeTrajectoryEvent(w http.ResponseWriter, r *http.Request, id string, exists func(context.Context) error) {
	if err := exists(r.Context()); err != nil {
		a.writeRunLookupError(w, err, "campaign")
		return
	}
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "seq must be a number")
		return
	}

	row, err := a.traces.RunEvent(r.Context(), id, seq)
	if errors.Is(err, trace.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "trajectory event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load trajectory event")
		return
	}
	writeJSON(w, http.StatusOK, trajectoryEventFull{
		trajectoryEvent: trajectoryEvent{
			Seq: row.Seq, At: row.At, Kind: row.Kind, Name: row.Name, Status: row.Status,
			Summary: row.Summary, DurationMS: row.DurationMS, PromptTokens: row.PromptTokens,
			CompletionTokens: row.CompletionTokens, HasPayload: row.HasPayload, Error: row.Error,
		},
		Payload: rawPayload(row.Payload),
	})
}

// writeRunLookupError разделяет «прогона нет» и «стор сломался». Сентинелы у
// доменов свои (campaign.ErrNotFound, review.ErrNotFound), поэтому проверяем оба.
func (a *API) writeRunLookupError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, campaign.ErrNotFound) || errors.Is(err, review.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", what+" not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", "could not load "+what)
}

// rawPayload отдаёт payload как есть, если это валидный JSON: иначе фронт получил
// бы строку вместо объекта. Битый payload (теоретически невозможен — мы сами его
// сериализуем) отдаём строкой, чтобы не ломать ответ.
func rawPayload(payload string) json.RawMessage {
	if payload == "" {
		return nil
	}
	if json.Valid([]byte(payload)) {
		return json.RawMessage(payload)
	}
	quoted, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return json.RawMessage(quoted)
}
