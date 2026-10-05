package tracing

import (
	"context"
	"fmt"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	wordstat "github.com/977ADAM/marketing-agents/internal/features/topic/source/wordstat"
	"strconv"
	"strings"
	"time"

	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// TracingSource оборачивает источник спроса (topicservice.Source) и пишет обращения в
// трассу прогона: что спрашивали, что вернулось и сколько заняло.
//
// В режиме full в payload попадают параметры и результат целиком (они небольшие),
// но заголовки авторизации не пишутся никогда — их тут и нет.
//
// География (wordstat.Regions) в порт не входит: её запрашивает только дымовой тест, и он
// работает с конкретным клиентом напрямую, без декоратора.
type TracingSource struct {
	inner topicservice.Source
	rec   trace.Recorder
}

// NewTracing оборачивает источник. Рекордер nil-безопасен.
func NewWordstat(inner topicservice.Source, rec trace.Recorder) *TracingSource {
	return &TracingSource{inner: inner, rec: trace.OrNop(rec)}
}

// Demand записывает спрос по фразе.
func (s *TracingSource) Demand(ctx context.Context, p topic.DemandParams) (topic.Demand, error) {
	start := time.Now()
	demand, err := s.inner.Demand(ctx, p)

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
		payload["totalCount"] = demand.TotalCount
		payload["hasData"] = demand.HasData
		payload["cacheHit"] = demand.CacheHit
		payload["requests"] = len(demand.Requests)
		payload["result"] = demand
		payload["associations"] = len(demand.Associations)
		ev.Summary = fmt.Sprintf("top_requests «%s»%s: %s показов, %d фраз",
			p.Phrase, regionNote(p.Regions), humanCount(demand.TotalCount), len(demand.Requests))
		if !demand.HasData {
			ev.Summary = fmt.Sprintf("top_requests «%s»%s: спроса нет", p.Phrase, regionNote(p.Regions))
		}
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.rec.Event(final, ev)
	return demand, err
}

// Dynamics записывает сезонность.
func (s *TracingSource) Dynamics(ctx context.Context, p topic.DynamicsParams) (topic.Dynamics, error) {
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
		payload["result"] = dyn
		ev.Summary = fmt.Sprintf("dynamics «%s»%s: %d точек сезонности",
			p.Phrase, regionNote(p.Regions), len(dyn.Points))
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.rec.Event(final, ev)
	return dyn, err
}

// Regions записывает географию спроса: в порт topicservice.Source она не входит, поэтому
// декоратор поддерживает её, только если внутренний источник умеет.
func (s *TracingSource) Regions(ctx context.Context, p wordstat.RegionsParams) (*wordstat.Regions, error) {
	inner, ok := s.inner.(interface {
		Regions(context.Context, wordstat.RegionsParams) (*wordstat.Regions, error)
	})
	if !ok {
		return nil, fmt.Errorf("wordstat: источник не умеет wordstat.Regions")
	}
	start := time.Now()
	regions, err := inner.Regions(ctx, p)

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
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.rec.Event(final, ev)
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

func (s *TracingSource) ConsumesToolBudget() bool {
	b, ok := s.inner.(interface{ ConsumesToolBudget() bool })
	return ok && b.ConsumesToolBudget()
}
