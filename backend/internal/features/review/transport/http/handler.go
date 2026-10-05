package reviewhttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	response "github.com/977ADAM/marketing-agents/internal/core/transport/http/response"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

// --- API: проверка готовых текстов ---

type createReviewReq struct {
	ClientID string                `json:"client_id"`
	Brief    string                `json:"brief"`
	Texts    []review.TextToReview `json:"texts"`
}

func (a *Handler) postReview(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow() {
		response.WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	var req createReviewReq
	if !response.DecodeJSON(w, r, a.limits.MaxJSONBytes, &req) {
		return
	}

	if strings.TrimSpace(req.Brief) == "" {
		response.WriteError(w, http.StatusBadRequest, "validation", "brief is required")
		return
	}
	if len(req.Texts) == 0 {
		response.WriteError(w, http.StatusBadRequest, "validation", "at least one text is required")
		return
	}
	for i, t := range req.Texts {
		if strings.TrimSpace(t.Body) == "" {
			response.WriteError(w, http.StatusBadRequest, "validation", fmt.Sprintf("text #%d has empty body", i+1))
			return
		}
	}
	id, err := a.reviews.Create(r.Context(), req.ClientID, review.Request{BriefText: req.Brief, Texts: req.Texts})
	if err != nil {
		var validation *limits.ValidationError
		if errors.As(err, &validation) {
			response.WriteError(w, http.StatusBadRequest, "validation", validation.Error())
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not create review")
		return
	}
	response.WriteJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "pending"})
}

func (a *Handler) getReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rev, err := a.reviews.GetCheck(r.Context(), id)
	if err == review.ErrNotFound {
		response.WriteError(w, http.StatusNotFound, "not_found", "review not found")
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not load review")
		return
	}
	response.WriteJSON(w, http.StatusOK, rev)
}

func (a *Handler) listReviews(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	items, err := a.reviews.ListChecks(r.Context(), limit)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not list reviews")
		return
	}
	response.WriteJSON(w, http.StatusOK, items)
}

func (a *Handler) reviewEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.reviews.GetCheck(r.Context(), id); err == review.ErrNotFound {
		response.WriteError(w, http.StatusNotFound, "not_found", "review not found")
		return
	} else if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not load review")
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

	snap, ch, cancel := a.sub.SubscribeReview(id)
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

// --- Разбор .docx (word/document.xml) без внешних зависимостей ---

// docxExtractReq лимиты на размер входа/выхода, чтобы не тащить гигабайты.
const (
	maxDocxUpload = 20 << 20  // 20 МБ на zip-файл
	maxDocxText   = 512 << 10 // 512 КБ извлечённого текста
)

// extractResponse — результат разбора .docx: title (первая строка), body
// (остальной текст — то, что уходит в поле «текст статьи») и полный text.
type extractResponse struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Text  string `json:"text"`
}

func (a *Handler) extractDocx(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow() {
		response.WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocxUpload)
	file, _, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "no_file", "multipart field 'file' (.docx) is required")
		return
	}
	defer file.Close()

	buf, err := io.ReadAll(io.LimitReader(file, maxDocxUpload+1))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal", "could not read upload")
		return
	}
	if len(buf) > maxDocxUpload {
		response.WriteError(w, http.StatusBadRequest, "too_large", "file exceeds 20MB")
		return
	}
	text, err := reviewservice.ExtractDOCXText(buf)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "bad_docx", "not a valid .docx: "+err.Error())
		return
	}
	body := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		body = text[i+1:]
	}
	response.WriteJSON(w, http.StatusOK, extractResponse{Title: firstLine(text), Body: body, Text: text})
}

// extractDocxText вытаскивает текст из document.xml архива .docx.

func firstLine(s string) string {
	if j := strings.IndexByte(s, '\n'); j >= 0 {
		return s[:j]
	}
	return s
}

type ReviewService interface {
	Create(context.Context, string, review.Request) (string, error)
	GetCheck(context.Context, string) (*review.Record, error)
	ListChecks(context.Context, int) ([]review.Summary, error)
}
type Subscriber interface {
	SubscribeReview(string) (run.Snapshot, <-chan run.Snapshot, func())
}
type Handler struct {
	reviews ReviewService
	sub     Subscriber
	limiter *middleware.RateLimiter
	limits  limits.Limits
}

func NewHandler(s ReviewService, sub Subscriber, limiter *middleware.RateLimiter, opt ...limits.Limits) *Handler {
	l := limits.Defaults()
	if len(opt) > 0 {
		l = limits.Normalize(opt[0])
	}
	return &Handler{reviews: s, sub: sub, limiter: limiter, limits: l}
}

func (h *Handler) Routes() []server.Route {
	return []server.Route{{"POST /api/reviews", h.postReview}, {"GET /api/reviews", h.listReviews}, {"GET /api/reviews/{id}", h.getReview}, {"GET /api/reviews/{id}/events", h.reviewEvents}, {"POST /api/reviews/extract", h.extractDocx}}
}

type ExtractResponse = extractResponse
