package trace

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSink записывает события в память и умеет падать по требованию.
type fakeSink struct {
	mu      sync.Mutex
	records []Record
	err     error
	calls   int
}

func (f *fakeSink) SaveRunEvent(_ context.Context, rec Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, rec)
	return nil
}

func (f *fakeSink) all() []Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Record(nil), f.records...)
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// runCtx помечает контекст прогоном: события без него не пишутся.
func runCtx(runID string) context.Context {
	return WithRunID(context.Background(), runID)
}

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":        ModeSummary,
		"off":     ModeOff,
		"summary": ModeSummary,
		"full":    ModeFull,
		"FULL":    ModeFull,
		" full ":  ModeFull,
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseMode("verbose"); err == nil {
		t.Error("ожидалась ошибка на неизвестном режиме")
	}
}

func TestNopWritesNothing(t *testing.T) {
	sink := &fakeSink{}
	rec := OrNop(nil)
	if rec.Enabled() {
		t.Error("Nop не должен считаться включённым")
	}
	rec.Event(runCtx("run-1"), Event{Kind: KindLLM, Name: "strategist"})
	if sink.calls != 0 {
		t.Error("Nop не должен писать в хранилище")
	}

	var nilRec *recorder
	OrNop(nilRec).Event(runCtx("run-1"), Event{}) // не должно паниковать
}

func TestOffModeSkipsSink(t *testing.T) {
	sink := &fakeSink{}
	rec := New(sink, Config{Mode: ModeOff})
	if rec.Enabled() {
		t.Error("в режиме off трасса выключена")
	}
	rec.Event(runCtx("run-1"), Event{Kind: KindLLM, Name: "strategist"})
	if sink.calls != 0 {
		t.Errorf("в режиме off записей быть не должно, получили %d", sink.calls)
	}
}

func TestSeqIsMonotonicPerRun(t *testing.T) {
	sink := &fakeSink{}
	rec := New(sink, Config{Mode: ModeSummary})
	rec.Event(runCtx("run-a"), Event{Kind: KindLLM, Name: "seeds"})
	rec.Event(runCtx("run-b"), Event{Kind: KindLLM, Name: "seeds"})
	rec.Event(runCtx("run-a"), Event{Kind: KindLLM, Name: "cluster"})
	rec.Event(runCtx("run-b"), Event{Kind: KindDecision, Name: "select"})
	rec.Event(runCtx("run-a"), Event{Kind: KindResult, Name: "done"})

	want := map[string][]int64{"run-a": {1, 2, 3}, "run-b": {1, 2}}
	for runID, seqs := range want {
		var got []int64
		for _, rec := range sink.all() {
			if rec.RunID == runID {
				got = append(got, rec.Seq)
			}
		}
		if len(got) != len(seqs) {
			t.Fatalf("%s: событий %d, want %d", runID, len(got), len(seqs))
		}
		for i := range seqs {
			if got[i] != seqs[i] {
				t.Errorf("%s: seq = %v, want %v", runID, got, seqs)
				break
			}
		}
	}
}

func TestSummaryDropsPayloadAndFullKeepsIt(t *testing.T) {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	summarySink := &fakeSink{}
	summary := New(summarySink, Config{Mode: ModeSummary, Now: fixedClock(at)})
	summary.Event(runCtx("run-1"), Event{
		Kind: KindLLM, Name: "copywriter",
		Payload: map[string]string{"system": "ты копирайтер", "user": "бриф"},
	})
	rec := summarySink.all()[0]
	if rec.PayloadJSON != "" {
		t.Errorf("в summary payload писать нельзя, получили %q", rec.PayloadJSON)
	}
	if !rec.At.Equal(at) {
		t.Errorf("At = %v, want %v", rec.At, at)
	}

	fullSink := &fakeSink{}
	full := New(fullSink, Config{Mode: ModeFull})
	full.Event(runCtx("run-1"), Event{
		Kind: KindLLM, Name: "copywriter",
		Payload: map[string]string{"system": "ты копирайтер"},
	})
	fullRec := fullSink.all()[0]
	if !strings.Contains(fullRec.PayloadJSON, "ты копирайтер") {
		t.Errorf("в full payload должен сохраняться, получили %q", fullRec.PayloadJSON)
	}
}

func TestPayloadTruncated(t *testing.T) {
	sink := &fakeSink{}
	rec := New(sink, Config{Mode: ModeFull, MaxPayloadBytes: 40})
	rec.Event(runCtx("run-1"), Event{
		Kind: KindLLM, Name: "copywriter",
		Payload: map[string]string{"body": strings.Repeat("ш", 200)},
	})

	got := sink.all()[0].PayloadJSON
	if len(got) > 40+len(truncateMark)+4 { // +4: закрывающая кавычка и скобка JSON
		t.Errorf("payload не обрезан: %d байт", len(got))
	}
	if !strings.Contains(got, truncateMark) {
		t.Errorf("нет пометки об обрезке: %q", got)
	}
}

func TestStatusDefaultsToOK(t *testing.T) {
	sink := &fakeSink{}
	rec := New(sink, Config{Mode: ModeSummary})
	rec.Event(runCtx("run-1"), Event{Kind: KindLLM, Name: "seeds"})
	if got := sink.all()[0].Status; got != StatusOK {
		t.Errorf("Status = %q, want %q", got, StatusOK)
	}
}

// Ошибка записи не должна ломать ни текущий, ни следующий вызов: трасса не имеет
// права влиять на прогон.
func TestSinkErrorDoesNotBreakRecording(t *testing.T) {
	sink := &fakeSink{err: errors.New("диск переполнен")}
	var reported []error
	rec := New(sink, Config{Mode: ModeSummary, OnError: func(err error) { reported = append(reported, err) }})
	rec.Event(runCtx("run-1"), Event{Kind: KindLLM, Name: "seeds"})
	rec.Event(runCtx("run-1"), Event{Kind: KindLLM, Name: "cluster"})

	if sink.calls != 2 {
		t.Errorf("вызовов хранилища %d, want 2", sink.calls)
	}
	if len(reported) != 2 {
		t.Errorf("об ошибках сообщено %d раз, want 2", len(reported))
	}
	if got := sink.all(); len(got) != 0 {
		t.Errorf("при ошибке записи событий быть не должно, получили %d", len(got))
	}
}

func TestEventCarriesMetrics(t *testing.T) {
	sink := &fakeSink{}
	rec := New(sink, Config{Mode: ModeSummary})
	rec.Event(runCtx("run-1"), Event{
		Kind: KindLLM, Name: "copywriter", Summary: "статья про шины",
		DurationMS: 1234, PromptTokens: 500, CompletionTokens: 900,
	})

	got := sink.all()[0]
	if got.DurationMS != 1234 || got.PromptTokens != 500 || got.CompletionTokens != 900 {
		t.Errorf("метрики потерялись: %+v", got)
	}
	if got.Summary != "статья про шины" {
		t.Errorf("summary = %q", got.Summary)
	}
}

// Событие без прогона в контексте не пишется: привязать его не к чему.
func TestNoRunIDSkipsSink(t *testing.T) {
	sink := &fakeSink{}
	rec := New(sink, Config{Mode: ModeSummary})
	rec.Event(context.Background(), Event{Kind: KindLLM, Name: "seeds"})
	if sink.calls != 0 {
		t.Errorf("без run_id записей быть не должно, получили %d", sink.calls)
	}
	if got := RunIDFrom(context.Background()); got != "" {
		t.Errorf("RunIDFrom пустого контекста = %q, want пусто", got)
	}
	if got := RunIDFrom(WithRunID(context.Background(), "run-7")); got != "run-7" {
		t.Errorf("RunIDFrom = %q, want run-7", got)
	}
}
