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

	"golang.org/x/time/rate"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// Repo — то, что API нужно от стора.
type Repo interface {
	Create(ctx context.Context, clientID string, b campaign.Brief) (string, error)
	Get(ctx context.Context, id string) (*store.Campaign, error)
	ListRecent(ctx context.Context, limit int) ([]store.CampaignSummary, error)
	CreateReview(ctx context.Context, clientID, briefText string) (string, error)
	GetReview(ctx context.Context, id string) (*store.Review, error)
	ListReviews(ctx context.Context, limit int) ([]store.ReviewSummary, error)
	// Трасса прогона: лента событий и одно событие с телом.
	RunEvents(ctx context.Context, runID string, limit int) ([]store.RunEventRow, error)
	RunEvent(ctx context.Context, runID string, seq int64) (*store.RunEventRow, error)
}

// Runner запускает фоновый прогон кампании или проверки текстов (асинхронно).
type Runner interface {
	Start(id string, b campaign.Brief)
	StartReview(id string, req review.Request)
}

// Subscriber — источник снимков прогресса для SSE.
type Subscriber interface {
	Subscribe(id string) (run.Snapshot, <-chan run.Snapshot, func())
	SubscribeReview(id string) (run.Snapshot, <-chan run.Snapshot, func())
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
	mux.HandleFunc("GET /api/campaigns/{id}/trajectory", a.campaignTrajectory)
	mux.HandleFunc("GET /api/campaigns/{id}/trajectory/{seq}", a.campaignTrajectoryEvent)
	mux.HandleFunc("POST /api/reviews", a.postReview)
	mux.HandleFunc("GET /api/reviews", a.listReviews)
	mux.HandleFunc("GET /api/reviews/{id}", a.getReview)
	mux.HandleFunc("GET /api/reviews/{id}/events", a.reviewEvents)
	mux.HandleFunc("GET /api/reviews/{id}/trajectory", a.reviewTrajectory)
	mux.HandleFunc("GET /api/reviews/{id}/trajectory/{seq}", a.reviewTrajectoryEvent)
	mux.HandleFunc("POST /api/reviews/extract", a.extractDocx)
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
	if req.Region != "" && !validGeoID(req.Region) {
		writeError(w, http.StatusBadRequest, "validation", "region must be a numeric Yandex geo id (for example 225)")
		return
	}
	if req.TopicsCount != 0 && (req.TopicsCount < 1 || req.TopicsCount > maxTopicsCount) {
		writeError(w, http.StatusBadRequest, "validation", fmt.Sprintf("topics_count must be between 1 and %d", maxTopicsCount))
		return
	}
	brief := campaign.Brief{
		Product: req.Product, Goal: req.Goal, Audience: req.Audience, Tone: req.Tone,
		Region: req.Region, TopicsCount: req.TopicsCount,
	}
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

func writeSSE(w http.ResponseWriter, event string, snap run.Snapshot) {
	b, _ := json.Marshal(snap)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
