// Package httpapi — REST-слой: создание/чтение кампаний, healthz.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/store"
	"golang.org/x/time/rate"
)

// Repo — то, что API нужно от стора.
type Repo interface {
	Create(ctx context.Context, clientID string, b agents.Brief) (string, error)
	Get(ctx context.Context, id string) (*store.Campaign, error)
}

// Runner запускает фоновый прогон кампании (асинхронно).
type Runner interface {
	Start(id string, b agents.Brief)
}

type API struct {
	repo    Repo
	runner  Runner
	limiter *rate.Limiter
}

func New(repo Repo, runner Runner, ratePerMin int) *API {
	lim := rate.NewLimiter(rate.Limit(float64(ratePerMin)/60.0), ratePerMin)
	return &API{repo: repo, runner: runner, limiter: lim}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /campaigns", a.postCampaign)
	mux.HandleFunc("GET /campaigns/{id}", a.getCampaign)
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
