package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/migrate"
	"github.com/977ADAM/marketing-agents/internal/store"
	"github.com/977ADAM/marketing-agents/internal/trace"
)

func event(runID string, seq int64, at time.Time, payload string) trace.Record {
	return trace.Record{
		RunID: runID, Seq: seq, At: at,
		Kind: trace.KindLLM, Name: "copywriter", Status: trace.StatusOK,
		DurationMS: 1200, PromptTokens: 100, CompletionTokens: 200,
		Summary: "статья про зимние шины", PayloadJSON: payload,
	}
}

func TestRunEventsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 3, 12, 30, 45, 123000000, time.UTC)

	if err := s.SaveRunEvent(ctx, event("run-1", 1, at, `{"system":"промпт"}`)); err != nil {
		t.Fatalf("SaveRunEvent: %v", err)
	}
	if err := s.SaveRunEvent(ctx, event("run-1", 2, at.Add(time.Second), "")); err != nil {
		t.Fatalf("SaveRunEvent: %v", err)
	}
	if err := s.SaveRunEvent(ctx, event("run-2", 1, at, "")); err != nil {
		t.Fatalf("SaveRunEvent: %v", err)
	}

	list, err := s.RunEvents(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("событий %d, want 2", len(list))
	}
	if list[0].Seq != 1 || list[1].Seq != 2 {
		t.Errorf("порядок по seq нарушен: %d, %d", list[0].Seq, list[1].Seq)
	}
	// В ленте payload не отдаём, но признак наличия тела нужен интерфейсу.
	if list[0].Payload != "" {
		t.Errorf("в ленте payload быть не должно: %q", list[0].Payload)
	}
	if !list[0].HasPayload {
		t.Error("у первого события должен быть признак payload")
	}
	if list[1].HasPayload {
		t.Error("у второго события payload нет")
	}
	if list[0].Kind != string(trace.KindLLM) || list[0].Status != string(trace.StatusOK) {
		t.Errorf("вид/статус потерялись: %+v", list[0])
	}
	if list[0].DurationMS != 1200 || list[0].PromptTokens != 100 || list[0].CompletionTokens != 200 {
		t.Errorf("метрики потерялись: %+v", list[0])
	}
	if list[0].Summary != "статья про зимние шины" {
		t.Errorf("summary = %q", list[0].Summary)
	}
	// Время должно пережить запись и чтение (формат, который разбирает драйвер).
	if !list[0].At.UTC().Equal(at) {
		t.Errorf("At = %v, want %v", list[0].At.UTC(), at)
	}

	// Детальный запрос отдаёт payload.
	full, err := s.RunEvent(ctx, "run-1", 1)
	if err != nil {
		t.Fatalf("RunEvent: %v", err)
	}
	if full.Payload != `{"system":"промпт"}` {
		t.Errorf("payload = %q", full.Payload)
	}

	// Чужой прогон и несуществующий seq — не находка.
	if _, err := s.RunEvent(ctx, "run-2", 1); err != nil {
		t.Errorf("своё событие второго прогона должно читаться: %v", err)
	}
	if _, err := s.RunEvent(ctx, "run-1", 99); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := s.RunEvent(ctx, "run-2", 2); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("чужой seq: err = %v, want ErrNotFound", err)
	}
}

// Ретенция удаляет только старое и не трогает сами прогоны.
func TestDeleteRunEventsBefore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	old := event("run-1", 1, now.AddDate(0, 0, -40), "")
	fresh := event("run-1", 2, now.AddDate(0, 0, -1), "")
	for _, ev := range []trace.Record{old, fresh} {
		if err := s.SaveRunEvent(ctx, ev); err != nil {
			t.Fatalf("SaveRunEvent: %v", err)
		}
	}
	id, err := s.Create(ctx, "", agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	deleted, err := s.DeleteRunEventsBefore(ctx, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("DeleteRunEventsBefore: %v", err)
	}
	if deleted != 1 {
		t.Errorf("удалено %d событий, want 1", deleted)
	}

	left, err := s.RunEvents(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	if len(left) != 1 || left[0].Seq != 2 {
		t.Errorf("осталось %+v, want только свежее событие", left)
	}
	// Прогон на месте.
	if _, err := s.Get(ctx, id); err != nil {
		t.Errorf("кампания должна остаться: %v", err)
	}
}

// События переживают перезапуск, а повторный Open по тому же файлу не падает и
// повторно схему не мигрирует (учёт ведёт golang-migrate в schema_migrations).
func TestRunEventsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/events.db"
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		s, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		if i == 0 {
			if err := migrate.Up(ctx, s.DB()); err != nil {
				t.Fatalf("migrate.Up: %v", err)
			}
		}
		if err := s.SaveRunEvent(ctx, event("run-1", int64(i+1), time.Now().UTC(), "")); err != nil {
			t.Fatalf("SaveRunEvent #%d: %v", i+1, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open после перезапуска: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := migrate.Up(ctx, s.DB()); err != nil {
		t.Fatalf("повторный migrate.Up: %v", err)
	}
	list, err := s.RunEvents(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("события не пережили перезапуск: %d", len(list))
	}
}
