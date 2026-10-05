// Package httpapi — REST-слой: создание/чтение кампаний, healthz.
package http

import (
	"encoding/json"
	"fmt"
	"golang.org/x/time/rate"
	"net/http"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
	"github.com/977ADAM/marketing-agents/internal/trace"
)

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
	campaigns campaign.Store
	reviews   review.Store
	traces    trace.Store
	runner    Runner
	sub       Subscriber
	limiter   *rate.Limiter
}

// New собирает транспорт: порты хранения приходят извне (реализация — internal/store),
// поэтому транспорт не знает ни про SQL, ни про конкретный стор.
func New(campaigns campaign.Store, reviews review.Store, traces trace.Store, runner Runner, sub Subscriber, ratePerMin int) *API {
	lim := rate.NewLimiter(rate.Limit(float64(ratePerMin)/60.0), ratePerMin)
	return &API{campaigns: campaigns, reviews: reviews, traces: traces, runner: runner, sub: sub, limiter: lim}
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

func writeSSE(w http.ResponseWriter, event string, snap run.Snapshot) {
	b, _ := json.Marshal(snap)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
