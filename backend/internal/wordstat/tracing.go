package wordstat

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/trace"
)

// TracingSource оборачивает источник спроса и пишет обращения в трассу прогона:
// инструмент, параметры запроса, что вернулось и сколько заняло.
//
// В режиме full в payload попадают параметры и результат целиком (они небольшие),
// но заголовки авторизации не пишутся никогда — их тут и нет.
type TracingSource struct {
	inner Source
	rec   trace.Recorder
}

// NewTracing оборачивает источник. Рекордер nil-безопасен.
func NewTracing(inner Source, rec trace.Recorder) *TracingSource {
	return &TracingSource{inner: inner, rec: trace.OrNop(rec)}
}

// TopRequests записывает спрос по фразе.
func (s *TracingSource) TopRequests(ctx context.Context, p TopParams) (*Top, error) {
	start := time.Now()
	top, err := s.inner.TopRequests(ctx, p)

	payload := map[string]any{
		"phrase":     p.Phrase,
		"regions":    p.Regions,
		"devices":    p.Devices,
		"numPhrases": p.NumPhrases,
	}
	ev := trace.Event{
		Kind:       trace.KindWordstat,
		Name:       "top_requests",
		Status:     trace.StatusOK,
		DurationMS: time.Since(start).Milliseconds(),
		Payload:    payload,
	}
	if err != nil {
		ev.Status = trace.StatusError
		ev.Error = err.Error()
		ev.Summary = fmt.Sprintf("top_requests «%s»: ошибка", p.Phrase)
	} else {
		payload["totalCount"] = top.TotalCount
		payload["hasData"] = top.HasData
		payload["cacheHit"] = top.CacheHit
		payload["requests"] = len(top.Requests)
		payload["associations"] = len(top.Associations)
		ev.Summary = fmt.Sprintf("top_requests «%s»%s: %s показов, %d фраз",
			p.Phrase, regionNote(p.Regions), humanCount(top.TotalCount), len(top.Requests))
		if !top.HasData {
			ev.Summary = fmt.Sprintf("top_requests «%s»%s: спроса нет", p.Phrase, regionNote(p.Regions))
		}
	}
	s.rec.Event(ctx, ev)
	return top, err
}

// Dynamics записывает сезонность.
func (s *TracingSource) Dynamics(ctx context.Context, p DynamicsParams) (*Dynamics, error) {
	start := time.Now()
	dyn, err := s.inner.Dynamics(ctx, p)

	payload := map[string]any{
		"phrase": p.Phrase, "period": p.Period,
		"fromDate": p.FromDate, "toDate": p.ToDate, "regions": p.Regions,
	}
	ev := trace.Event{
		Kind:       trace.KindWordstat,
		Name:       "dynamics",
		Status:     trace.StatusOK,
		DurationMS: time.Since(start).Milliseconds(),
		Payload:    payload,
	}
	if err != nil {
		ev.Status = trace.StatusError
		ev.Error = err.Error()
		ev.Summary = fmt.Sprintf("dynamics «%s»: ошибка", p.Phrase)
	} else {
		payload["points"] = len(dyn.Points)
		ev.Summary = fmt.Sprintf("dynamics «%s»%s: %d точек сезонности",
			p.Phrase, regionNote(p.Regions), len(dyn.Points))
	}
	s.rec.Event(ctx, ev)
	return dyn, err
}

// Regions записывает географию спроса.
func (s *TracingSource) Regions(ctx context.Context, p RegionsParams) (*Regions, error) {
	start := time.Now()
	regions, err := s.inner.Regions(ctx, p)

	payload := map[string]any{
		"phrase": p.Phrase, "regionMode": p.RegionMode, "includeNames": p.IncludeNames,
	}
	ev := trace.Event{
		Kind:       trace.KindWordstat,
		Name:       "regions",
		Status:     trace.StatusOK,
		DurationMS: time.Since(start).Milliseconds(),
		Payload:    payload,
	}
	if err != nil {
		ev.Status = trace.StatusError
		ev.Error = err.Error()
		ev.Summary = fmt.Sprintf("regions «%s»: ошибка", p.Phrase)
	} else {
		payload["regions"] = len(regions.Items)
		ev.Summary = fmt.Sprintf("regions «%s»: %d регионов", p.Phrase, len(regions.Items))
	}
	s.rec.Event(ctx, ev)
	return regions, err
}

// regionNote добавляет регион в описание события, если фильтр задан.
func regionNote(regions []string) string {
	if len(regions) == 0 {
		return ""
	}
	return " [" + strings.Join(regions, ",") + "]"
}

// humanCount разделяет тысячи пробелами: в ленте трассы так читается легче.
func humanCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
