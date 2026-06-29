// Package httpapi — REST-слой: создание/чтение кампаний, healthz.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
	"golang.org/x/time/rate"
)

// Repo — то, что API нужно от стора.
type Repo interface {
	Create(ctx context.Context, clientID string, b agents.Brief) (string, error)
	Get(ctx context.Context, id string) (*store.Campaign, error)
	ListRecent(ctx context.Context, limit int) ([]store.CampaignSummary, error)
}

// Runner запускает фоновый прогон кампании (асинхронно).
type Runner interface {
	Start(id string, b agents.Brief)
}

// Subscriber — источник снимков прогресса для SSE.
type Subscriber interface {
	Subscribe(id string) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func())
}

type API struct {
	repo    Repo
	runner  Runner
	sub     Subscriber
	limiter *rate.Limiter
}

func New(repo Repo, runner Runner, sub Subscriber, ratePerMin int) *API {
	lim := rate.NewLimiter(rate.Limit(float64(ratePerMin)/60.0), ratePerMin)
	return &API{repo: repo, runner: runner, sub: sub, limiter: lim}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/campaigns", a.postCampaign)
	mux.HandleFunc("GET /api/campaigns", a.listCampaigns)
	mux.HandleFunc("GET /api/campaigns/{id}", a.getCampaign)
	mux.HandleFunc("GET /api/campaigns/{id}/events", a.campaignEvents)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

type createReq struct {
	ClientID string `json:"client_id"`
	Product  string `json:"product"`
	Goal     string `json:"goal"`
	Audience string `json:"audience"`
	Tone     string `json:"tone"`
}

func (a *API) postCampaign(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow() {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Product) == "" || strings.TrimSpace(req.Goal) == "" ||
		strings.TrimSpace(req.Audience) == "" || strings.TrimSpace(req.Tone) == "" {
		writeError(w, http.StatusBadRequest, "validation", "product, goal, audience, tone are required")
		return
	}
	brief := agents.Brief{Product: req.Product, Goal: req.Goal, Audience: req.Audience, Tone: req.Tone}
	id, err := a.repo.Create(r.Context(), req.ClientID, brief)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create campaign")
		return
	}
	a.runner.Start(id, brief)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "pending"})
}

func (a *API) getCampaign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := a.repo.Get(r.Context(), id)
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load campaign")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *API) listCampaigns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	items, err := a.repo.ListRecent(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list campaigns")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) campaignEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.repo.Get(r.Context(), id); err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load campaign")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // не буферизировать SSE за nginx

	snap, ch, cancel := a.sub.Subscribe(id)
	defer cancel()
	writeSSE(w, "", snap)
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
				writeSSE(w, "done", last)
				flusher.Flush()
				return
			}
			last = s
			writeSSE(w, "", s)
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, snap orchestrator.Snapshot) {
	b, _ := json.Marshal(snap)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
