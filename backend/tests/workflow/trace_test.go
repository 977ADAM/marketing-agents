package workflow_test

import (
	"context"
	"errors"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"

	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
	"strings"
	"testing"
)

// captureTrace собирает события трассы для проверок.
type captureTrace struct {
	events []trace.Event
}

func (c *captureTrace) Event(_ context.Context, ev trace.Event) { c.events = append(c.events, ev) }
func (c *captureTrace) Enabled() bool                           { return true }

func (c *captureTrace) byName(name string) []trace.Event {
	var out []trace.Event
	for _, ev := range c.events {
		if ev.Name == name {
			out = append(out, ev)
		}
	}
	return out
}

func (c *captureTrace) summaries() string {
	var b strings.Builder
	for _, ev := range c.events {
		b.WriteString(ev.Summary)
		b.WriteString("\n")
	}
	return b.String()
}

// runCtx помечает контекст прогоном: без этого трасса молчит.
func runCtx() context.Context { return trace.WithRunID(context.Background(), "run-1") }

// happyCampaignFakes настраивает фейковую модель на полный успешный прогон.
func happyCampaignFakes(t *testing.T) *mock.LLM {
	t.Helper()
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина","какую зимнюю резину"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины","goal":"поймать в момент выбора","task":"дать чек-лист",
		 "queries":["какую зимнюю резину"],"selected":true},
		{"title":"Сколько стоит зимняя резина","goal":"поймать перед покупкой","task":"дать ориентир",
		 "queries":["купить зимнюю резину"],"selected":true}]}`}
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"надёжность зимой","topics":[{"title":"Из стратега","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{
		`{"topic":"t","title":"A1","body":"b1","cta":"c1"}`,
		`{"topic":"t","title":"A2","body":"b2","cta":"c2"}`,
	}
	fake.Responses[campaignservice.RoleCritic] = []string{
		`{"score":90,"issues":[],"verdict":"accept"}`,
		`{"score":88,"issues":[],"verdict":"accept"}`,
	}
	return fake
}

func TestRunEmitsDecisionTrail(t *testing.T) {
	src := winterSource()
	fake := happyCampaignFakes(t)
	rec := &captureTrace{}

	opt := researchOptions(src, campaignservice.Options{})
	opt.Recorder = rec
	o := campaignservice.NewWorkflow(fake, opt)

	if _, err := o.Run(runCtx(), researchBrief(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Сеялки, сбор спроса по каждой и кластеризация.
	if got := rec.byName("seeds"); len(got) != 1 || !strings.Contains(got[0].Summary, "2") {
		t.Errorf("событие о сеялках: %+v", got)
	}
	if got := rec.byName("seed_collected"); len(got) != 2 {
		t.Errorf("событий о сеялках %d, want 2: %+v", len(got), got)
	} else if !strings.Contains(got[0].Summary, "зимняя резина") {
		t.Errorf("в сводке нет фразы: %q", got[0].Summary)
	}
	if got := rec.byName("selection"); len(got) != 1 || !strings.Contains(got[0].Summary, "2 тем") {
		t.Errorf("событие о кластеризации: %+v", got)
	}

	// Решение по каждому кандидату: и выбранной, и отклонённой теме.
	decisions := rec.byName("topic_decision")
	if len(decisions) != 2 {
		t.Fatalf("решений по темам %d, want 2", len(decisions))
	}
	var selected, rejected int
	for _, ev := range decisions {
		payload, _ := ev.Payload.(map[string]any)
		if payload["selected"] == true {
			selected++
			if !strings.Contains(ev.Summary, "выбрана моделью") {
				t.Errorf("сводка выбранной темы = %q", ev.Summary)
			}
		} else {
			rejected++
			if !strings.Contains(ev.Summary, "модель не выбрала") {
				t.Errorf("сводка отклонённой темы = %q", ev.Summary)
			}
		}
		// Порогов в решении больше нет: их место занял выбор модели.
		if _, ok := payload["min_volume"]; ok {
			t.Errorf("в решении остался порог кода: %#v", payload)
		}
		if payload["volume"] == nil {
			t.Errorf("в решении нет данных по цитатам: %#v", payload)
		}
	}
	if selected != 2 || rejected != 0 {
		t.Errorf("выбрано моделью %d, отклонено %d (want 2/0)", selected, rejected)
	}

	// Итерации критика и итог прогона.
	if got := rec.byName("critic"); len(got) != 2 {
		t.Errorf("событий критика %d, want 2: %s", len(got), rec.summaries())
	}
	res := rec.byName("run")
	if len(res) != 1 {
		t.Fatalf("итоговых событий %d, want 1", len(res))
	}
	if res[0].Status != trace.StatusOK || !strings.Contains(res[0].Summary, "Wordstat 4") {
		t.Errorf("итог = %q (status %q)", res[0].Summary, res[0].Status)
	}
	payload, _ := res[0].Payload.(map[string]any)
	if payload["wordstat_calls"] != 4 || payload["deliverables"] != 2 {
		t.Errorf("итоговый payload = %#v", payload)
	}
}

// Итерации критика раньше терялись: теперь по каждой есть запись с оценкой.
func TestRunEmitsCriticIterations(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[campaignservice.RoleStrategist] = []string{
		`{"positioning":"p","topics":[{"title":"T1","angle":"a","points":["x"]}]}`,
	}
	fake.Responses[campaignservice.RoleCopywriter] = []string{
		`{"topic":"T1","title":"v1","body":"b","cta":"c"}`,
		`{"topic":"T1","title":"v2","body":"b","cta":"c"}`,
	}
	fake.Responses[campaignservice.RoleCritic] = []string{
		`{"score":50,"issues":["слабый заход","нет цифр"],"verdict":"revise"}`,
		`{"score":85,"issues":[],"verdict":"accept"}`,
	}
	rec := &captureTrace{}
	o := campaignservice.NewWorkflow(fake, campaignservice.Options{CriticMaxIter: 3, ScoreThreshold: 80, Recorder: rec})

	if _, err := o.Run(runCtx(), brief(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := rec.byName("critic")
	if len(got) != 2 {
		t.Fatalf("событий критика %d, want 2", len(got))
	}
	if !strings.Contains(got[0].Summary, "итерация 1") || !strings.Contains(got[0].Summary, "50/100") {
		t.Errorf("первая итерация = %q", got[0].Summary)
	}
	if !strings.Contains(got[0].Summary, "на доработку") || !strings.Contains(got[0].Summary, "замечаний 2") {
		t.Errorf("в первой итерации нет вердикта или замечаний: %q", got[0].Summary)
	}
	if !strings.Contains(got[1].Summary, "итерация 2") || !strings.Contains(got[1].Summary, "принято") {
		t.Errorf("вторая итерация = %q", got[1].Summary)
	}
	// Замечания видны целиком: по ним понятно, что правил копирайтер.
	payload, _ := got[0].Payload.(map[string]any)
	issues, _ := payload["issues"].([]string)
	if len(issues) != 2 {
		t.Errorf("замечания не сохранены: %#v", payload)
	}
}

// Провалившийся прогон тоже оставляет итог — иначе трасса не объясняет сбой.
func TestRunEmitsFailedResult(t *testing.T) {
	src := mock.NewWordstat()
	src.Err = errors.New("MCP недоступен")
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}

	rec := &captureTrace{}
	opt := researchOptions(src, campaignservice.Options{})
	opt.Recorder = rec
	o := campaignservice.NewWorkflow(fake, opt)

	if _, err := o.Run(runCtx(), researchBrief(), nil); err == nil {
		t.Fatal("ожидалась ошибка прогона")
	}

	res := rec.byName("run")
	if len(res) != 1 {
		t.Fatalf("итоговых событий %d, want 1", len(res))
	}
	if res[0].Status != trace.StatusError || !strings.Contains(res[0].Error, "MCP недоступен") {
		t.Errorf("итог об ошибке = %+v", res[0])
	}
}

// sinkSpy — подменённое хранилище трассы: считает записи.
type sinkSpy struct {
	records []trace.Record
}

func (s *sinkSpy) SaveRunEvent(_ context.Context, rec trace.Record) error {
	s.records = append(s.records, rec)
	return nil
}

func (s *sinkSpy) names() []string {
	out := make([]string, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, rec.Name)
	}
	return out
}

// Без прогона в контексте трасса молчит: события не к чему привязать.
func TestRunWithoutRunIDWritesNothing(t *testing.T) {
	sink := &sinkSpy{}
	rec := traceservice.New(sink, trace.Config{Mode: trace.ModeSummary})

	opt := researchOptions(winterSource(), campaignservice.Options{})
	opt.Recorder = rec
	o := campaignservice.NewWorkflow(happyCampaignFakes(t), opt)

	if _, err := o.Run(context.Background(), researchBrief(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.records) != 0 {
		t.Errorf("без run_id записей быть не должно, получили %v", sink.names())
	}
}

// С прогоном в контексте вся цепочка работает: оркестратор → рекордер → хранилище.
func TestRunWithRunIDWritesTrail(t *testing.T) {
	sink := &sinkSpy{}
	rec := traceservice.New(sink, trace.Config{Mode: trace.ModeSummary})

	opt := researchOptions(winterSource(), campaignservice.Options{})
	opt.Recorder = rec
	o := campaignservice.NewWorkflow(happyCampaignFakes(t), opt)

	if _, err := o.Run(runCtx(), researchBrief(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.records) == 0 {
		t.Fatal("ожидались события трассы")
	}
	// seq монотонный, а run_id проставлен в каждом событии.
	for i, r := range sink.records {
		if r.RunID != "run-1" {
			t.Fatalf("RunID = %q, want run-1", r.RunID)
		}
		if r.Seq != int64(i+1) {
			t.Errorf("seq = %d, want %d", r.Seq, i+1)
		}
	}
	names := sink.names()
	for _, want := range []string{"seeds", "seed_collected", "selection", "topic_decision", "critic", "run"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("в трассе нет события %q; есть %v", want, names)
		}
	}
}
