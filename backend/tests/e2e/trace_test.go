package e2e_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/httpapi"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
	"github.com/977ADAM/marketing-agents/internal/trace"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
)

// Сквозная проверка трассы: раннер помечает прогон, агенты и инструменты пишут
// события через декораторы, рекордер складывает их в БД с тем же run_id.
func TestRunnerWritesTrajectory(t *testing.T) {
	ctx := context.Background()
	// Схему готовит отдельный сервис миграций; тест повторяет этот шаг явно.
	db, err := store.OpenDB(ctx, t.TempDir()+"/trace.db")
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	applyMigrations(t, db)
	st := store.New(db)
	t.Cleanup(func() { _ = st.Close() })

	rec := trace.New(st, trace.Config{Mode: trace.ModeSummary})

	fake := llm.NewFake()
	fake.Responses[agents.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[agents.RoleCluster] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины","goal":"поймать в момент выбора","task":"дать чек-лист",
		 "queries":["какую зимнюю резину"]}]}`}
	fake.Responses[agents.RoleStrategist] = []string{`{"positioning":"надёжность зимой","topics":[{"title":"S","angle":"a","points":["x"]}]}`}
	fake.Responses[agents.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[agents.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	src := wordstat.NewFake()
	src.SetTop("зимняя резина", wordstat.Seed("зимняя резина", 1028481, map[string]int64{
		"зимняя резина":       1028481,
		"какую зимнюю резину": 92398,
	}))

	orch := orchestrator.New(llm.NewTracing(fake, rec), orchestrator.Options{
		CriticMaxIter: 1, ScoreThreshold: 80,
		Wordstat:         wordstat.NewTracing(src, rec),
		Recorder:         rec,
		Select:           orchestrator.SelectOptions{MinVolume: 300, SeasonalityFactor: 3},
		TopicsMultiplier: 2,
		SeedCount:        1,
		MaxWordstatCalls: 5,
		DefaultRegion:    "225",
	})
	hub := httpapi.NewHub(ctx, st)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := httpapi.NewRunner(ctx, st, orch, 30*time.Second, logger, hub)

	brief := campaign.Brief{
		Product: "Зимняя резина", Goal: "рост продаж", Audience: "автовладельцы",
		Tone: "экспертный", Region: "213", TopicsCount: 1,
	}
	id, err := st.Create(ctx, "", brief)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	runner.Start(id, brief)

	events := waitForResult(t, st, id)

	// Все события прогона привязаны к кампании — иначе трассу не найти через API.
	names := map[string]int{}
	for _, ev := range events {
		names[ev.Name]++
	}
	for _, want := range []string{
		agents.RoleSeeds, agents.RoleCluster, agents.RoleStrategist, agents.RoleCopywriter,
		"top_requests", "seeds", "seed_collected", "clustering", "topic_decision", "critic", "run",
	} {
		if names[want] == 0 {
			t.Errorf("в трассе нет события %q; есть: %v", want, names)
		}
	}

	// В режиме summary тела не пишутся — это проверяем на событии вызова модели.
	for _, ev := range events {
		if ev.Name != agents.RoleSeeds {
			continue
		}
		full, err := st.RunEvent(ctx, id, ev.Seq)
		if err != nil {
			t.Fatalf("RunEvent: %v", err)
		}
		if full.Payload != "" {
			t.Errorf("в summary payload писать нельзя: %q", full.Payload)
		}
		break
	}

	// Регион из брифа дошёл до источника — виден в событии Wordstat.
	if len(src.TopParamsLog) == 0 || src.TopParamsLog[0].Regions[0] != "213" {
		t.Errorf("регион не ушёл в источник: %+v", src.TopParamsLog)
	}
}

// waitForResult ждёт появления итогового события прогона.
func waitForResult(t *testing.T, st *store.Store, runID string) []store.RunEventRow {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		events, err := st.RunEvents(ctx, runID, 0)
		if err != nil {
			t.Fatalf("RunEvents: %v", err)
		}
		for _, ev := range events {
			if ev.Name == "run" {
				return events
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("прогон не завершился за 15 секунд")
	return nil
}
