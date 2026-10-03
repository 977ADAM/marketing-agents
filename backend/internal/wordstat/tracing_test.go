package wordstat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/trace"
)

// captureRecorder собирает события трассы для проверок.
type captureRecorder struct {
	events []trace.Event
}

func (c *captureRecorder) Event(_ context.Context, ev trace.Event) { c.events = append(c.events, ev) }
func (c *captureRecorder) Enabled() bool                           { return true }

func ctxWithRun() context.Context { return trace.WithRunID(context.Background(), "run-1") }

func TestTracingSourceRecordsTopRequests(t *testing.T) {
	rec := &captureRecorder{}
	src := NewTracing(winterSourceForTrace(), rec)

	top, err := src.TopRequests(ctxWithRun(), TopParams{Phrase: "зимняя резина", NumPhrases: 50, Regions: []string{"213"}})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}
	if top.TotalCount != 1028481 {
		t.Fatalf("источник вернул %d", top.TotalCount)
	}

	ev := rec.events[0]
	if ev.Kind != trace.KindWordstat || ev.Name != "top_requests" {
		t.Errorf("вид/имя = %q/%q", ev.Kind, ev.Name)
	}
	if ev.Status != trace.StatusOK {
		t.Errorf("Status = %q", ev.Status)
	}
	// В описании — фраза, регион и объём с разделителями тысяч.
	for _, want := range []string{"зимняя резина", "213", "1 028 481"} {
		if !strings.Contains(ev.Summary, want) {
			t.Errorf("summary = %q, нет %q", ev.Summary, want)
		}
	}

	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", ev.Payload)
	}
	if payload["totalCount"] != int64(1028481) || payload["hasData"] != true {
		t.Errorf("результат не записан: %#v", payload)
	}
	if payload["numPhrases"] != 50 {
		t.Errorf("numPhrases не записан: %#v", payload)
	}
	if payload["requests"] != 2 {
		t.Errorf("число популярных фраз = %v, want 2", payload["requests"])
	}
}

func TestTracingSourceRecordsNoDemand(t *testing.T) {
	rec := &captureRecorder{}
	src := NewTracing(NewFake(), rec) // Fake по умолчанию отдаёт «спроса нет»

	if _, err := src.TopRequests(ctxWithRun(), TopParams{Phrase: "ыфвыфв ыфва"}); err != nil {
		t.Fatalf("TopRequests: %v", err)
	}
	ev := rec.events[0]
	if !strings.Contains(ev.Summary, "спроса нет") {
		t.Errorf("summary = %q, want про отсутствие спроса", ev.Summary)
	}
	if ev.Status != trace.StatusOK {
		t.Errorf("отсутствие спроса — не ошибка, статус %q", ev.Status)
	}
}

func TestTracingSourceRecordsDynamicsAndRegions(t *testing.T) {
	rec := &captureRecorder{}
	fake := NewFake()
	fake.DynamicsR = &Dynamics{Phrase: "зимняя резина", Points: []DynamicsPoint{{Date: "2026-09-01T00:00:00Z", Count: 10}}}
	fake.RegionsR = &Regions{Phrase: "аренда офиса", Items: []RegionItem{{RegionID: "225", Name: "Россия", Count: 45299}}}
	src := NewTracing(fake, rec)

	if _, err := src.Dynamics(ctxWithRun(), DynamicsParams{Phrase: "зимняя резина", Period: "monthly"}); err != nil {
		t.Fatalf("Dynamics: %v", err)
	}
	if _, err := src.Regions(ctxWithRun(), RegionsParams{Phrase: "аренда офиса", IncludeNames: true}); err != nil {
		t.Fatalf("Regions: %v", err)
	}

	if len(rec.events) != 2 {
		t.Fatalf("событий %d, want 2", len(rec.events))
	}
	if rec.events[0].Name != "dynamics" || !strings.Contains(rec.events[0].Summary, "1 точек") {
		t.Errorf("dynamics: %+v", rec.events[0])
	}
	if rec.events[1].Name != "regions" || !strings.Contains(rec.events[1].Summary, "1 регионов") {
		t.Errorf("regions: %+v", rec.events[1])
	}
}

func TestTracingSourceRecordsError(t *testing.T) {
	rec := &captureRecorder{}
	fake := NewFake()
	fake.Err = errors.New("MCP недоступен")
	src := NewTracing(fake, rec)

	if _, err := src.TopRequests(ctxWithRun(), TopParams{Phrase: "зимняя резина"}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	ev := rec.events[0]
	if ev.Status != trace.StatusError || !strings.Contains(ev.Error, "MCP недоступен") {
		t.Errorf("событие об ошибке = %+v", ev)
	}
}

// Без прогона в контексте события не пишутся, но вызовы работают.
func TestTracingSourceWithoutRunID(t *testing.T) {
	rec := &captureRecorder{}
	src := NewTracing(winterSourceForTrace(), rec)
	if _, err := src.TopRequests(context.Background(), TopParams{Phrase: "зимняя резина"}); err != nil {
		t.Fatalf("TopRequests: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("событий %d, want 1 (декоратор пишет, рекордер решает)", len(rec.events))
	}
	// А вот настоящий рекордер без run_id промолчит — это проверяется в trace.
	if trace.RunIDFrom(context.Background()) != "" {
		t.Error("контекст без прогона не должен содержать run_id")
	}
}

// humanCount разделяет тысячи и не ломается на отрицательных и малых числах.
func TestHumanCount(t *testing.T) {
	cases := map[int64]string{0: "0", 999: "999", 1000: "1 000", 1028481: "1 028 481", 12345: "12 345"}
	for in, want := range cases {
		if got := humanCount(in); got != want {
			t.Errorf("humanCount(%d) = %q, want %q", in, got, want)
		}
	}
}

// winterSourceForTrace — источник с числами из фикстур Wordstat.
func winterSourceForTrace() *Fake {
	fake := NewFake()
	fake.SetTop("зимняя резина", Seed("зимняя резина", 1028481, map[string]int64{
		"зимняя резина":        1028481,
		"купить зимнюю резину": 289429,
	}))
	return fake
}
