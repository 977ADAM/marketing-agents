package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/review"
)

// --- API: проверка готовых текстов ---

type createReviewReq struct {
	ClientID string                `json:"client_id"`
	Brief    string                `json:"brief"`
	Texts    []review.TextToReview `json:"texts"`
}

func (a *API) postReview(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow() {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	var req createReviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Brief) == "" {
		writeError(w, http.StatusBadRequest, "validation", "brief is required")
		return
	}
	if len(req.Texts) == 0 {
		writeError(w, http.StatusBadRequest, "validation", "at least one text is required")
		return
	}
	for i, t := range req.Texts {
		if strings.TrimSpace(t.Body) == "" {
			writeError(w, http.StatusBadRequest, "validation", fmt.Sprintf("text #%d has empty body", i+1))
			return
		}
	}
	id, err := a.reviews.CreateReview(r.Context(), req.ClientID, req.Brief)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create review")
		return
	}
	a.runner.StartReview(id, review.Request{BriefText: req.Brief, Texts: req.Texts})
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "pending"})
}

func (a *API) getReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rev, err := a.reviews.GetReview(r.Context(), id)
	if err == review.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "review not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load review")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (a *API) listReviews(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	items, err := a.reviews.ListReviews(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list reviews")
		return
	}
	// Заголовок для списка готовит API: фронт показывает то, что пришло.
	for i := range items {
		items[i].BriefTitle = firstLine(items[i].BriefText)
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) reviewEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.reviews.GetReview(r.Context(), id); err == review.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "review not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load review")
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

	snap, ch, cancel := a.sub.SubscribeReview(id)
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

func (a *API) extractDocx(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow() {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocxUpload)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no_file", "multipart field 'file' (.docx) is required")
		return
	}
	defer file.Close()

	buf, err := io.ReadAll(io.LimitReader(file, maxDocxUpload+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read upload")
		return
	}
	if len(buf) > maxDocxUpload {
		writeError(w, http.StatusBadRequest, "too_large", "file exceeds 20MB")
		return
	}
	text, err := extractDocxText(buf)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_docx", "not a valid .docx: "+err.Error())
		return
	}
	body := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		body = text[i+1:]
	}
	writeJSON(w, http.StatusOK, extractResponse{Title: firstLine(text), Body: body, Text: text})
}

// extractDocxText вытаскивает текст из document.xml архива .docx.
func extractDocxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		xmlBytes, err := io.ReadAll(io.LimitReader(rc, maxDocxText+1))
		if err != nil {
			return "", err
		}
		if len(xmlBytes) > maxDocxText {
			return "", errors.New("extracted text too large")
		}
		return parseDocxDocument(xmlBytes), nil
	}
	return "", errors.New("word/document.xml not found")
}

// parseDocxDocument собирает текст всех абзацев (w:p) документа, включая
// абзацы внутри таблиц (w:tbl/w:tr/w:tc). w:tab → '\t', w:br/w:cr → '\n'.
// Разбор идёт токенами, чтобы не зависеть от вложенности элементов.
func parseDocxDocument(xmlBytes []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	var out strings.Builder
	var line strings.Builder
	depth := 0      // вложенность в w:p
	inText := false // внутри w:t

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out.String()
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				depth++
			case "t":
				inText = true
			case "tab":
				if depth > 0 {
					line.WriteByte('\t')
				}
			case "br", "cr":
				if depth > 0 {
					line.WriteByte('\n')
				}
			}
		case xml.CharData:
			if inText && depth > 0 {
				line.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				depth--
				if depth == 0 {
					s := strings.TrimSpace(line.String())
					if s != "" {
						if out.Len() > 0 {
							out.WriteByte('\n')
						}
						out.WriteString(s)
					}
					line.Reset()
				}
			}
		}
	}
	return out.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
