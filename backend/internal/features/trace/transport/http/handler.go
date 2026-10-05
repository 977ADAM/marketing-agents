package tracehttp

import (
	"context"
	"encoding/json"
	"errors"
	response "github.com/977ADAM/marketing-agents/internal/core/transport/http/response"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	"net/http"
	"strconv"
	"time"

	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
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
	NextSeq int64             `json:"next_seq"`
	HasMore bool              `json:"has_more"`
	ID      string            `json:"id"`
	Total   int               `json:"total"`
	Events  []trajectoryEvent `json:"events"`
}

func (a *Handler) campaignTrajectory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectory(w, r, id, func(ctx context.Context) error {
		_, err := a.campaigns.Get(ctx, id)
		return err
	})
}

func (a *Handler) campaignTrajectoryEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectoryEvent(w, r, id, func(ctx context.Context) error {
		_, err := a.campaigns.Get(ctx, id)
		return err
	})
}

func (a *Handler) reviewTrajectory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectory(w, r, id, func(ctx context.Context) error {
		_, err := a.reviews.GetCheck(ctx, id)
		return err
	})
}

func (a *Handler) reviewTrajectoryEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.writeTrajectoryEvent(w, r, id, func(ctx context.Context) error {
		_, err := a.reviews.GetCheck(ctx, id)
		return err
	})
}

// writeTrajectory отдаёт ленту событий прогона. exists проверяет, что прогон есть:
// иначе трасса несуществующей кампании выглядела бы как «пустая».
func (a *Handler) writeTrajectory(w http.ResponseWriter, r *http.Request, id string, exists func(context.Context) error) {
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

	after := int64(0)
	if v := r.URL.Query().Get("after_seq"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			response.WriteError(w, http.StatusBadRequest, "validation", "after_seq must be nonnegative")
			return
		}
		after = n
	}
	page, err := a.traces.RunEventsPage(r.Context(), id, after, limit)
	rows := page.Rows
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not load trajectory")
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
	response.WriteJSON(w, http.StatusOK, trajectoryResponse{ID: id, Total: page.Total, NextSeq: page.NextSeq, HasMore: page.HasMore, Events: events})
}

// writeTrajectoryEvent отдаёт одно событие вместе с телом.
func (a *Handler) writeTrajectoryEvent(w http.ResponseWriter, r *http.Request, id string, exists func(context.Context) error) {
	if err := exists(r.Context()); err != nil {
		a.writeRunLookupError(w, err, "campaign")
		return
	}
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation", "seq must be a number")
		return
	}

	row, err := a.traces.RunEvent(r.Context(), id, seq)
	if errors.Is(err, trace.ErrNotFound) {
		response.WriteError(w, http.StatusNotFound, "not_found", "trajectory event not found")
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not load trajectory event")
		return
	}
	response.WriteJSON(w, http.StatusOK, trajectoryEventFull{
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
func (a *Handler) writeRunLookupError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, campaign.ErrNotFound) || errors.Is(err, review.ErrNotFound) {
		response.WriteError(w, http.StatusNotFound, "not_found", what+" not found")
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "internal", "could not load "+what)
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

type CampaignReader interface {
	Get(context.Context, string) (*campaign.Record, error)
}
type ReviewReader interface {
	GetCheck(context.Context, string) (*review.Record, error)
}
type TraceQuery interface {
	RunEventsPage(context.Context, string, int64, int) (trace.Page, error)
	RunEvents(context.Context, string, int) ([]trace.Row, error)
	RunEvent(context.Context, string, int64) (*trace.Row, error)
}
type Handler struct {
	campaigns CampaignReader
	reviews   ReviewReader
	traces    TraceQuery
}

func NewHandler(c CampaignReader, r ReviewReader, q TraceQuery) *Handler { return &Handler{c, r, q} }
func (h *Handler) Routes() []server.Route {
	return []server.Route{{Pattern: "GET /api/campaigns/{id}/trajectory", Handler: h.campaignTrajectory}, {Pattern: "GET /api/campaigns/{id}/trajectory/{seq}", Handler: h.campaignTrajectoryEvent}, {Pattern: "GET /api/reviews/{id}/trajectory", Handler: h.reviewTrajectory}, {Pattern: "GET /api/reviews/{id}/trajectory/{seq}", Handler: h.reviewTrajectoryEvent}}
}
