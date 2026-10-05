// Package httpapi — REST-слой: создание/чтение кампаний, healthz.
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/campaign"
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
	id, err := a.campaigns.Create(r.Context(), req.ClientID, brief)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create campaign")
		return
	}
	a.runner.Start(id, brief)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "pending"})
}

func (a *API) getCampaign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := a.campaigns.Get(r.Context(), id)
	if err == campaign.ErrNotFound {
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
	items, err := a.campaigns.ListRecent(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list campaigns")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) campaignEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.campaigns.Get(r.Context(), id); err == campaign.ErrNotFound {
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
