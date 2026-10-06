package e2e_test

import (
	"context"
	"encoding/json"
	sloglogger "github.com/977ADAM/marketing-agents/internal/adapters/logger/slog"
	tracing "github.com/977ADAM/marketing-agents/internal/adapters/tracing"
	campaignrepo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	reviewrepo "github.com/977ADAM/marketing-agents/internal/features/review/repository/mariadb"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	tracerepo "github.com/977ADAM/marketing-agents/internal/features/trace/repository/mariadb"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"

	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
	testdb "github.com/977ADAM/marketing-agents/internal/testkit/testdb"
)

// Сквозная проверка трассы: раннер помечает прогон, агенты и инструменты пишут
// события через декораторы, рекордер складывает их в БД с тем же run_id.
func TestRunnerWritesTrajectory(t *testing.T) {
	ctx := context.Background()
	// Схему готовит отдельный сервис миграций; testdb повторяет этот шаг на
	// временной базе.
	db, _ := testdb.New(t)
	campaigns := campaignrepo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	evStore := tracerepo.NewEvents(db)

	rec := traceservice.New(evStore, trace.Config{Mode: trace.ModeSummary})

	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины","goal":"поймать в момент выбора","task":"дать чек-лист",
		 "queries":["какую зимнюю резину"],"selected":true}]}`}
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"надёжность зимой","topics":[{"title":"S","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[campaignservice.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	src := mock.NewWordstat()
	src.SetTop("зимняя резина", mock.Seed("зимняя резина", 1028481, map[string]int64{
		"зимняя резина":       1028481,
		"какую зимнюю резину": 92398,
	}))

	orch := campaignservice.NewWorkflow(tracing.NewLLM(fake, rec), campaignservice.Options{
		CriticMaxIter: 1, ScoreThreshold: 80,
		Wordstat:         tracing.NewWordstat(src, rec),
		Recorder:         rec,
		SeedCount:        1,
		MaxWordstatCalls: 5,
		DefaultRegion:    "225",
	})
	hub := runner.NewHub(ctx, campaigns, reviews)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := runner.NewRunner(ctx, campaigns, reviews, orch, reviewservice.NewWorkflow(tracing.NewLLM(fake, rec), reviewservice.Options{}), 30*time.Second, sloglogger.New(logger), hub)

	brief := campaign.Brief{
		Product: "Зимняя резина", Goal: "рост продаж", Audience: "автовладельцы",
		Tone: "экспертный", Region: "213", TopicsCount: 1,
	}
	id, err := campaigns.Create(ctx, "", brief)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	runner.Start(id, brief)

	rows := waitForResult(t, evStore, id)

	// Все события прогона привязаны к кампании — иначе трассу не найти через API.
	names := map[string]int{}
	for _, ev := range rows {
		names[ev.Name]++
	}
	for _, want := range []string{
		topicservice.RoleSeeds, topicservice.RoleSelect, campaignservice.RoleStrategist, campaignservice.RoleCopywriter,
		"top_requests", "seeds", "seed_collected", "selection", "topic_decision", "critic", "run",
	} {
		if names[want] == 0 {
			t.Errorf("в трассе нет события %q; есть: %v", want, names)
		}
	}

	// В режиме summary тела не пишутся — это проверяем на событии вызова модели.
	for _, ev := range rows {
		if ev.Name != topicservice.RoleSeeds {
			continue
		}
		full, err := evStore.RunEvent(ctx, id, ev.Seq)
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

// Размышления модели доходят до хранилища трассы: в теле события лежит сам текст
// размышлений, в summary — их счётчик. Проверяем на том же сквозном пути
// (раннер → агенты → декораторы → БД), что и остальную трассу.
func TestRunnerWritesReasoningToTrajectory(t *testing.T) {
	ctx := context.Background()
	db, _ := testdb.New(t)
	campaigns := campaignrepo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	evStore := tracerepo.NewEvents(db)

	rec := traceservice.New(evStore, trace.Config{Mode: trace.ModeFull})

	fake := mock.NewLLM()
	fake.Reasoning = "Сначала выберу сеялку по брифу"
	fake.ReasoningTokens = 42
	fake.FinishReason = "stop"
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины","goal":"поймать в момент выбора","task":"дать чек-лист",
		 "queries":["какую зимнюю резину"],"selected":true}]}`}
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"надёжность зимой","topics":[{"title":"S","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[campaignservice.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	src := mock.NewWordstat()
	src.SetTop("зимняя резина", mock.Seed("зимняя резина", 1028481, map[string]int64{
		"зимняя резина":       1028481,
		"какую зимнюю резину": 92398,
	}))

	orch := campaignservice.NewWorkflow(tracing.NewLLM(fake, rec), campaignservice.Options{
		CriticMaxIter: 1, ScoreThreshold: 80,
		Wordstat:         tracing.NewWordstat(src, rec),
		Recorder:         rec,
		SeedCount:        1,
		MaxWordstatCalls: 5,
		DefaultRegion:    "225",
	})
	hub := runner.NewHub(ctx, campaigns, reviews)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobRunner := runner.NewRunner(ctx, campaigns, reviews, orch, reviewservice.NewWorkflow(tracing.NewLLM(fake, rec), reviewservice.Options{}), 30*time.Second, sloglogger.New(logger), hub)

	brief := campaign.Brief{
		Product: "Зимняя резина", Goal: "рост продаж", Audience: "автовладельцы",
		Tone: "экспертный", Region: "213", TopicsCount: 1,
	}
	id, err := campaigns.Create(ctx, "", brief)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jobRunner.Start(id, brief)
	rows := waitForResult(t, evStore, id)

	var llmEvent *trace.Row
	for i := range rows {
		if rows[i].Kind == string(trace.KindLLM) {
			llmEvent = &rows[i]
			break
		}
	}
	if llmEvent == nil {
		t.Fatalf("в трассе нет вызовов модели: %+v", rows)
	}
	if !strings.Contains(llmEvent.Summary, "размышления") || !strings.Contains(llmEvent.Summary, "42") {
		t.Errorf("summary = %q, want счётчик размышлений", llmEvent.Summary)
	}

	full, err := evStore.RunEvent(ctx, id, llmEvent.Seq)
	if err != nil {
		t.Fatalf("RunEvent: %v", err)
	}
	// Тело хранится в конверте {data,truncated}: разворачиваем, как это делает UI.
	var envelope struct {
		Data      json.RawMessage `json:"data"`
		Truncated bool            `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(full.Payload), &envelope); err != nil {
		t.Fatalf("конверт payload: %v (%q)", err, full.Payload)
	}
	var payload struct {
		Reasoning       string   `json:"reasoning"`
		ReasoningTokens int      `json:"reasoning_tokens"`
		FinishReason    string   `json:"finish_reason"`
		Response        string   `json:"response"`
		TruncatedFields []string `json:"truncated_fields"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("тело события: %v (%q)", err, envelope.Data)
	}
	if payload.Reasoning != fake.Reasoning {
		t.Errorf("размышления потерялись: %q", payload.Reasoning)
	}
	if payload.ReasoningTokens != 42 || payload.FinishReason != "stop" {
		t.Errorf("метаданные вызова: %+v", payload)
	}
	if payload.Response == "" {
		t.Error("ответ модели не записан")
	}
	if len(payload.TruncatedFields) != 0 {
		t.Errorf("короткое тело не должно обрезаться: %v", payload.TruncatedFields)
	}
}

// waitForResult ждёт появления итогового события прогона.
func waitForResult(t *testing.T, events *tracerepo.Events, runID string) []trace.Row {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := events.RunEvents(ctx, runID, 0)
		if err != nil {
			t.Fatalf("RunEvents: %v", err)
		}
		for _, ev := range rows {
			if ev.Name == "run" {
				return rows
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("прогон не завершился за 15 секунд")
	return nil
}
