// Package httpapi — REST-слой: создание/чтение кампаний, healthz.
package campaignhttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	response "github.com/977ADAM/marketing-agents/internal/core/transport/http/response"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	"net/http"
	"strconv"
	"strings"
	"time"

	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
)

// validGeoID проверяет geo ID Яндекса: непустая строка из цифр.
func validGeoID(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (a *Handler) postCampaign(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow() {
		response.WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	var req createReq
	if !response.DecodeJSON(w, r, a.limits.MaxJSONBytes, &req) {
		return
	}

	if strings.TrimSpace(req.Product) == "" || strings.TrimSpace(req.Goal) == "" ||
		strings.TrimSpace(req.Audience) == "" || strings.TrimSpace(req.Tone) == "" {
		response.WriteError(w, http.StatusBadRequest, "validation", "product, goal, audience, tone are required")
		return
	}
	if req.Region != "" && !validGeoID(req.Region) {
		response.WriteError(w, http.StatusBadRequest, "validation", "region must be a numeric Yandex geo id (for example 225)")
		return
	}
	if req.TopicsCount != 0 && (req.TopicsCount < 1 || req.TopicsCount > a.limits.MaxTopics) {
		response.WriteError(w, http.StatusBadRequest, "validation", fmt.Sprintf("topics_count must be between 1 and %d", a.limits.MaxTopics))
		return
	}
	brief := campaign.Brief{
		Product: req.Product, Goal: req.Goal, Audience: req.Audience, Tone: req.Tone,
		Region: req.Region, TopicsCount: req.TopicsCount,
	}
	id, err := a.campaigns.Create(run.WithIdempotencyKey(r.Context(), r.Header.Get("Idempotency-Key")), req.ClientID, brief)
	if err != nil {
		if errors.Is(err, run.ErrIdempotencyConflict) {
			response.WriteError(w, http.StatusConflict, "idempotency_conflict", err.Error())
			return
		}
		if errors.Is(err, run.ErrBusy) || errors.Is(err, run.ErrStopping) {
			response.WriteError(w, http.StatusServiceUnavailable, "busy", "background capacity unavailable")
			return
		}
		var validation *limits.ValidationError
		if errors.As(err, &validation) {
			response.WriteError(w, http.StatusBadRequest, "validation", validation.Error())
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not create campaign")
		return
	}
	response.WriteJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "pending"})
}

func (a *Handler) getCampaign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := a.campaigns.Get(r.Context(), id)
	if err == campaign.ErrNotFound {
		response.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not load campaign")
		return
	}
	response.WriteJSON(w, http.StatusOK, c)
}

func (a *Handler) listCampaigns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	items, err := a.campaigns.ListRecent(r.Context(), limit)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not list campaigns")
		return
	}
	response.WriteJSON(w, http.StatusOK, items)
}

func (a *Handler) campaignEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.campaigns.Get(r.Context(), id); err == campaign.ErrNotFound {
		response.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	} else if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not load campaign")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // не буферизировать SSE за nginx

	snap, ch, cancel := a.sub.Subscribe(id)
	defer cancel()
	response.WriteSSE(w, "", snap)
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	last := snap
	for {
		select {
		case <-r.Context().Done():
			return
		case s, ok := <-ch:
			// Канал закрыт = прогон завершён. last — последний доставленный снимок;
			// итоговую фазу/результат клиент дочитывает через GET /api/campaigns/{id}
			// (финальный снимок мог не дойти при переполненном буфере — см. tracker.finish).
			if !ok {
				response.WriteSSE(w, "done", last)
				flusher.Flush()
				return
			}
			last = s
			response.WriteSSE(w, "", s)
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

type createReq struct {
	ClientID string `json:"client_id"`
	Product  string `json:"product"`
	Goal     string `json:"goal"`
	Audience string `json:"audience"`
	Tone     string `json:"tone"`
	// Region — geo ID Яндекса для подбора тем (225 — Россия, 213 — Москва).
	// Пусто — регион по умолчанию из конфига.
	Region string `json:"region"`
	// TopicsCount — сколько статей нужно по медиаплану; идей подбираем вдвое
	// больше. 0 — значение по умолчанию.
	TopicsCount int `json:"topics_count"`
}

// maxTopicsCount — верхняя граница числа статей в одном брифе: защита от
// случайного «1000 статей» в поле.
const maxTopicsCount = 20

type CampaignService interface {
	Resume(context.Context, string) (string, error)
	Create(context.Context, string, campaign.Brief) (string, error)
	Get(context.Context, string) (*campaign.Record, error)
	ListRecent(context.Context, int) ([]campaign.Summary, error)
}
type Subscriber interface {
	Subscribe(string) (run.Snapshot, <-chan run.Snapshot, func())
}
type Handler struct {
	campaigns CampaignService
	sub       Subscriber
	limiter   *middleware.RateLimiter
	limits    limits.Limits
}

func NewHandler(s CampaignService, sub Subscriber, limiter *middleware.RateLimiter, opt ...limits.Limits) *Handler {
	l := limits.Defaults()
	if len(opt) > 0 {
		l = limits.Normalize(opt[0])
	}
	return &Handler{campaigns: s, sub: sub, limiter: limiter, limits: l}
}

func (h *Handler) Routes() []server.Route {
	return []server.Route{{"POST /api/campaigns/{id}/retry", h.retry}, {"POST /api/campaigns", h.postCampaign}, {"GET /api/campaigns", h.listCampaigns}, {"GET /api/campaigns/{id}", h.getCampaign}, {"GET /api/campaigns/{id}/events", h.campaignEvents}}
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	if !h.limiter.Allow() {
		response.WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	id, err := h.campaigns.Resume(r.Context(), r.PathValue("id"))
	if errors.Is(err, run.ErrInvalidState) {
		response.WriteError(w, http.StatusConflict, "invalid_state", err.Error())
		return
	}
	if errors.Is(err, run.ErrResumeUnavailable) {
		response.WriteError(w, http.StatusConflict, "resume_unavailable", err.Error())
		return
	}
	if errors.Is(err, run.ErrBusy) || errors.Is(err, run.ErrStopping) {
		response.WriteError(w, http.StatusServiceUnavailable, "busy", err.Error())
		return
	}
	if errors.Is(err, campaign.ErrNotFound) {
		response.WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not resume run")
		return
	}
	response.WriteJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "pending"})
}
