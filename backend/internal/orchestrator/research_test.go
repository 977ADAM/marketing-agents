package orchestrator_test

import (
	"context"
	"errors"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	"strings"
	"testing"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
)

// researchProgress — recorder с поддержкой этапа подбора тем.
type researchProgress struct {
	*recordProgress
	stages []run.ResearchStage
	seeds  []string
	done   int
}

func newResearchProgress() *researchProgress {
	return &researchProgress{recordProgress: &recordProgress{}}
}

func (r *researchProgress) Researching(s run.ResearchStage) {
	r.mu.Lock()
	r.stages = append(r.stages, s)
	r.mu.Unlock()
	r.add("researching:" + string(s))
}

func (r *researchProgress) ResearchSeeds(seeds []string) {
	r.mu.Lock()
	r.seeds = append([]string(nil), seeds...)
	r.mu.Unlock()
	r.add("seeds:" + strings.Join(seeds, "|"))
}

func (r *researchProgress) ResearchSeedDone(int) {
	r.mu.Lock()
	r.done++
	r.mu.Unlock()
	r.add("seed-done")
}

// winterSource — источник с реальными числами из фикстур Wordstat.
func winterSource() *mock.Wordstat {
	src := mock.NewWordstat()
	src.SetTop("зимняя резина", mock.Seed("зимняя резина", 1028481, map[string]int64{
		"зимняя резина":             1028481,
		"купить зимнюю резину":      289429,
		"какую зимнюю резину":       92398,
		"какая зимняя резина лучше": 34038,
		"зимняя резина 205 55 16":   16880,
	}))
	src.SetTop("какую зимнюю резину", mock.Seed("какую зимнюю резину", 92398, map[string]int64{
		"какую зимнюю резину":       92398,
		"какая зимняя резина лучше": 34038,
	}))
	src.DynamicsR = &topic.Dynamics{Phrase: "зимняя резина", Period: "PERIOD_MONTHLY", Points: []topic.DynamicsPoint{
		{Date: "2025-12-01T00:00:00Z", Count: 900000},
		{Date: "2026-06-01T00:00:00Z", Count: 100000},
	}}
	return src
}

// researchBrief — бриф с регионом и числом статей.
func researchBrief() campaign.Brief {
	b := brief()
	b.Region = "213"
	b.TopicsCount = 2
	return b
}

func researchOptions(src topic.Source, opt orchestrator.Options) orchestrator.Options {
	opt.CriticMaxIter = 3
	opt.ScoreThreshold = 80
	opt.CostPer1KPrompt = 1
	opt.CostPer1KCompletion = 1
	opt.Wordstat = src
	opt.DefaultRegion = "225"
	return opt
}

// Выбор тем — решение модели: код отдаёт ей фразы с частотностями, а из ответа
// берёт ровно то, что она выбрала (объём и головную фразу считает по данным).
func TestRunResearchModelDecidesSelection(t *testing.T) {
	src := winterSource()
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина","какую зимнюю резину"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины: 6 простых правил","goal":"поймать в момент выбора",
		 "task":"дать чек-лист","intent":"выбор","selected":true,
		 "queries":["какую зимнюю резину","какая зимняя резина лучше"]},
		{"title":"Сколько стоит зимняя резина","goal":"поймать перед покупкой","task":"дать ориентир",
		 "intent":"коммерческий","selected":false,"reject":"объём мал для нашей задачи",
		 "queries":["купить зимнюю резину"]}]}`}
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"надёжность зимой","topics":[{"title":"Из стратега","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{`{"topic":"t","title":"A1","body":"b1","cta":"c1"}`}
	fake.Responses[campaignservice.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	p := newResearchProgress()
	o := orchestrator.New(fake, researchOptions(src, orchestrator.Options{}))
	res, err := o.Run(context.Background(), researchBrief(), p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// В работу ушла только выбранная моделью тема и в её порядке.
	if len(res.Strategy.Topics) != 1 {
		t.Fatalf("тем %d, want 1: %+v", len(res.Strategy.Topics), res.Strategy.Topics)
	}
	if got := res.Strategy.Topics[0].Title; got != "Как выбрать зимние шины: 6 простых правил" {
		t.Errorf("тема = %q", got)
	}

	// Кандидаты: данные по цитатам считает код, решение — модель.
	var selected, rejected int
	for _, c := range res.Strategy.TopicCandidates {
		switch {
		case c.Selected:
			selected++
			if c.Volume != 92398 {
				t.Errorf("объём выбранной темы = %d, want 92398 (максимум по цитатам)", c.Volume)
			}
			if c.Head != "какую зимнюю резину" {
				t.Errorf("головная фраза = %q", c.Head)
			}
			if c.Source != topic.SourceWordstat {
				t.Errorf("источник = %q, want wordstat", c.Source)
			}
			if c.Season == nil || !c.Season.Seasonal {
				t.Errorf("сезонность не собрана: %+v", c.Season)
			}
		default:
			rejected++
			if c.Reject != "объём мал для нашей задачи" {
				t.Errorf("причина отказа = %q, want из ответа модели", c.Reject)
			}
		}
	}
	if selected != 1 || rejected != 1 {
		t.Errorf("selected=%d rejected=%d, want 1 и 1", selected, rejected)
	}

	// Решение модели видно в трассе.
	if got := p.stages; len(got) == 0 || got[len(got)-1] != run.StageSelecting {
		t.Errorf("этапы подбора = %v, want завершение на selecting", got)
	}
}

// Спроса нет вовсе: модель Select не зовём, темы даёт fallback (source=llm).
func TestRunResearchFallsBackWhenNoDemand(t *testing.T) {
	src := mock.NewWordstat() // ни одной фразы — спроса нет
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[topicservice.RoleFallback] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины","goal":"g","task":"k","intent":"выбор"},
		{"title":"Когда менять шины","goal":"g","task":"k","intent":"вопрос"}]}`}
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"Из стратега","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{
		`{"topic":"t","title":"A1","body":"b1","cta":"c1"}`,
		`{"topic":"t","title":"A2","body":"b2","cta":"c2"}`,
	}
	fake.Responses[campaignservice.RoleCritic] = []string{
		`{"score":90,"issues":[],"verdict":"accept"}`,
		`{"score":90,"issues":[],"verdict":"accept"}`,
	}

	o := orchestrator.New(fake, researchOptions(src, orchestrator.Options{}))
	res, err := o.Run(context.Background(), researchBrief(), newResearchProgress())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := fake.Calls[topicservice.RoleSelect]; n != 0 {
		t.Errorf("Select вызван %d раз на пустых данных, want 0", n)
	}
	if len(res.Strategy.Topics) != 2 {
		t.Fatalf("тем %d, want 2 (fallback)", len(res.Strategy.Topics))
	}
	for _, c := range res.Strategy.TopicCandidates {
		if c.Source != topic.SourceLLM {
			t.Errorf("источник темы без данных = %q, want llm", c.Source)
		}
		if !c.Selected {
			t.Errorf("fallback-тема не выбрана: %+v", c)
		}
	}
}

// Данные есть, но модель не выбрала ни одной темы — это ошибка, а не тихий добор.
func TestRunResearchFailsWhenModelSelectsNothing(t *testing.T) {
	src := winterSource()
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Сколько стоит","goal":"g","task":"k","queries":["купить зимнюю резину"],
		 "selected":false,"reject":"не наша аудитория"}]}`}

	o := orchestrator.New(fake, researchOptions(src, orchestrator.Options{}))
	_, err := o.Run(context.Background(), researchBrief(), newResearchProgress())
	if err == nil || !strings.Contains(err.Error(), "не выбрала ни одной темы") {
		t.Fatalf("err = %v, want ошибку про пустой выбор модели", err)
	}
}

// Выдуманная цитата валит подбор: модель обязана ссылаться на реальные фразы.
func TestRunResearchFailsOnInventedCitation(t *testing.T) {
	src := winterSource()
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Летние шины","goal":"g","task":"k","queries":["летняя резина"],"selected":true}]}`}

	o := orchestrator.New(fake, researchOptions(src, orchestrator.Options{}))
	_, err := o.Run(context.Background(), researchBrief(), newResearchProgress())
	if !errors.Is(err, topicservice.ErrUnknownQuery) {
		t.Fatalf("err = %v, want ErrUnknownQuery", err)
	}
}

// Ошибка источника не подменяется выдумкой.
func TestRunResearchFailsOnSourceError(t *testing.T) {
	src := mock.NewWordstat()
	src.Err = errors.New("wordstat недоступен")
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}

	o := orchestrator.New(fake, researchOptions(src, orchestrator.Options{}))
	_, err := o.Run(context.Background(), researchBrief(), newResearchProgress())
	if err == nil || !strings.Contains(err.Error(), "wordstat недоступен") {
		t.Fatalf("err = %v, want ошибку источника", err)
	}
}

// Лимит обращений к Wordstat соблюдается.
func TestRunResearchRespectsCallLimit(t *testing.T) {
	src := winterSource()
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":["зимняя резина","какую зимнюю резину","купить зимнюю резину"]}`}
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"T","goal":"g","task":"k","queries":["какую зимнюю резину"],"selected":true}]}`}
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"Из стратега","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[campaignservice.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	o := orchestrator.New(fake, researchOptions(src, orchestrator.Options{MaxWordstatCalls: 1}))
	res, err := o.Run(context.Background(), researchBrief(), newResearchProgress())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if src.CallCount() != 1 {
		t.Errorf("обращений к Wordstat = %d, want 1 (лимит)", src.CallCount())
	}
	if res.Strategy.WordstatCalls != 1 {
		t.Errorf("WordstatCalls = %d, want 1", res.Strategy.WordstatCalls)
	}
}

// Без источника спроса подбор выключен: темы даёт стратег, как раньше.
func TestRunWithoutWordstatSkipsResearch(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"Из стратега","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[campaignservice.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	o := orchestrator.New(fake, orchestrator.Options{CriticMaxIter: 1, ScoreThreshold: 80, CostPer1KPrompt: 1, CostPer1KCompletion: 1})
	res, err := o.Run(context.Background(), brief(), newResearchProgress())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Strategy.Topics) != 1 || res.Strategy.Topics[0].Title != "Из стратега" {
		t.Fatalf("темы = %+v, want одну из стратега", res.Strategy.Topics)
	}
	if n := fake.Calls[topicservice.RoleSeeds]; n != 0 {
		t.Errorf("сеялки вызваны %d раз без Wordstat, want 0", n)
	}
}
