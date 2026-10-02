package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
)

// researchProgress — recorder с поддержкой этапа подбора тем.
type researchProgress struct {
	*recordProgress
	stages []ResearchStage
	seeds  []string
	done   int
}

func newResearchProgress() *researchProgress {
	return &researchProgress{recordProgress: &recordProgress{}}
}

func (r *researchProgress) Researching(s ResearchStage) {
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
func winterSource() *wordstat.Fake {
	src := wordstat.NewFake()
	src.SetTop("зимняя резина", wordstat.Seed("зимняя резина", 1028481, map[string]int64{
		"зимняя резина":             1028481,
		"купить зимнюю резину":      289429,
		"какую зимнюю резину":       92398,
		"какая зимняя резина лучше": 34038,
		"зимняя резина 205 55 16":   16880,
	}))
	src.SetTop("какую зимнюю резину", wordstat.Seed("какую зимнюю резину", 92398, map[string]int64{
		"какую зимнюю резину":       92398,
		"какая зимняя резина лучше": 34038,
	}))
	src.DynamicsR = &wordstat.Dynamics{Phrase: "зимняя резина", Period: "PERIOD_MONTHLY", Points: winterDynamics()}
	return src
}

// researchBrief — бриф с регионом и числом статей.
func researchBrief() agents.Brief {
	b := brief()
	b.Region = "213"
	b.TopicsCount = 2
	return b
}

func researchOptions(src wordstat.Source, opt Options) Options {
	opt.CriticMaxIter = 3
	opt.ScoreThreshold = 80
	opt.CostPer1KPrompt = 1
	opt.CostPer1KCompletion = 1
	opt.Wordstat = src
	opt.Select = SelectOptions{MinVolume: 300, SeasonalityFactor: 3}
	opt.TopicsMultiplier = 2
	opt.DefaultRegion = "225"
	return opt
}

func TestRunResearchUsesWordstatTopics(t *testing.T) {
	src := winterSource()
	fake := llm.NewFake()
	fake.Responses[agents.RoleSeeds] = []string{`{"seeds":["зимняя резина","какую зимнюю резину"]}`}
	fake.Responses[agents.RoleCluster] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины: 6 простых правил","goal":"поймать в момент выбора",
		 "task":"дать чек-лист","intent":"выбор",
		 "queries":["какую зимнюю резину","какая зимняя резина лучше"]},
		{"title":"Сколько стоит зимняя резина","goal":"поймать перед покупкой","task":"дать ориентир",
		 "intent":"коммерческий","queries":["купить зимнюю резину"]}]}`}
	fake.Responses[agents.RoleStrategist] = []string{`{"positioning":"надёжность зимой","topics":[{"title":"Из стратега","angle":"a","points":["x"]}]}`}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"t","title":"A1","body":"b1","cta":"c1"}`,
		`{"topic":"t","title":"A2","body":"b2","cta":"c2"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":90,"issues":[],"verdict":"accept"}`,
		`{"score":90,"issues":[],"verdict":"accept"}`,
	}

	p := newResearchProgress()
	o := New(fake, researchOptions(src, Options{}))
	res, err := o.Run(context.Background(), researchBrief(), p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Темы — из спроса, а не из стратега; позиционирование — от стратега.
	if len(res.Strategy.Topics) != 2 {
		t.Fatalf("тем %d, want 2: %+v", len(res.Strategy.Topics), res.Strategy.Topics)
	}
	if res.Strategy.Topics[0].Title != "Как выбрать зимние шины: 6 простых правил" {
		t.Errorf("первая тема = %q", res.Strategy.Topics[0].Title)
	}
	for _, tp := range res.Strategy.Topics {
		if tp.Title == "Из стратега" {
			t.Error("тема из стратега попала в генерацию вместо темы по спросу")
		}
	}
	if res.Strategy.Positioning != "надёжность зимой" {
		t.Errorf("Positioning = %q, want от стратега", res.Strategy.Positioning)
	}
	if len(res.Deliverables) != 2 {
		t.Errorf("deliverables = %d, want 2", len(res.Deliverables))
	}

	// Кандидаты: объём — максимум по цитатам, а не сумма; технический мусор не тема.
	cands := res.Strategy.TopicCandidates
	if len(cands) != 2 {
		t.Fatalf("кандидатов %d, want 2: %+v", len(cands), cands)
	}
	if cands[0].Volume != 92398 {
		t.Errorf("Volume = %d, want 92398 (максимум, не сумма 126 436)", cands[0].Volume)
	}
	if cands[0].Head != "какую зимнюю резину" {
		t.Errorf("Head = %q", cands[0].Head)
	}
	for _, c := range cands {
		if !c.Selected {
			t.Errorf("тема %q не отобрана", c.Title)
		}
		if c.Source != agents.SourceWordstat {
			t.Errorf("источник темы %q = %q, want wordstat", c.Title, c.Source)
		}
	}
	if res.Strategy.WordstatCalls != 4 { // 2 сеялки + 2 dynamics
		t.Errorf("WordstatCalls = %d, want 4", res.Strategy.WordstatCalls)
	}

	// Регион из брифа ушёл в фильтр спроса и сезонности.
	for _, p := range src.TopParamsLog {
		if len(p.Regions) != 1 || p.Regions[0] != "213" {
			t.Errorf("регион спроса = %v, want [213]", p.Regions)
		}
		if p.NumPhrases != DefaultNumPhrases {
			t.Errorf("NumPhrases = %d, want %d", p.NumPhrases, DefaultNumPhrases)
		}
	}
	if len(src.DynamicsParamsLog) == 0 || src.DynamicsParamsLog[0].Regions[0] != "213" {
		t.Errorf("регион сезонности = %+v", src.DynamicsParamsLog)
	}

	// Подэтапы и сеялки видны в прогрессе, все сеялки закрыты.
	wantStages := []ResearchStage{StageSeeds, StageFetching, StageClustering, StageSelecting}
	if len(p.stages) != len(wantStages) {
		t.Fatalf("подэтапы = %v, want %v", p.stages, wantStages)
	}
	for i := range wantStages {
		if p.stages[i] != wantStages[i] {
			t.Errorf("подэтап %d = %q, want %q", i, p.stages[i], wantStages[i])
		}
	}
	if len(p.seeds) != 2 {
		t.Errorf("сеялки в прогрессе = %v", p.seeds)
	}
	if p.done != 2 {
		t.Errorf("закрытых сеялок = %d, want 2", p.done)
	}
}

// Спроса нет вовсе: классификация не запускается, темы берём у модели.
func TestRunResearchFallsBackWhenNoDemand(t *testing.T) {
	src := wordstat.NewFake() // default: hasData=false
	fake := llm.NewFake()
	fake.Responses[agents.RoleSeeds] = []string{`{"seeds":["ыфвыфв ыфва"]}`}
	fake.Responses[agents.RoleFallback] = []string{`{"topics":[
		{"title":"Как подобрать размер","goal":"g","task":"t","intent":"выбор"},
		{"title":"Что учитывать при выборе","goal":"g","task":"t"}]}`}
	fake.Responses[agents.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"S","angle":"a","points":["x"]}]}`}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"t","title":"A1","body":"b1","cta":"c1"}`,
		`{"topic":"t","title":"A2","body":"b2","cta":"c2"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":90,"issues":[],"verdict":"accept"}`,
		`{"score":90,"issues":[],"verdict":"accept"}`,
	}

	o := New(fake, researchOptions(src, Options{}))
	res, err := o.Run(context.Background(), researchBrief(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Strategy.Topics) != 2 {
		t.Fatalf("тем %d, want 2", len(res.Strategy.Topics))
	}
	for _, c := range res.Strategy.TopicCandidates {
		if c.Source != agents.SourceLLM {
			t.Errorf("тема %q: source = %q, want llm", c.Title, c.Source)
		}
		if c.Volume != 0 || len(c.Queries) != 0 {
			t.Errorf("у темы без данных не должно быть цифр: %+v", c)
		}
	}
	if fake.Calls[agents.RoleCluster] != 0 {
		t.Error("кластеризация не нужна, когда фраз не собрано")
	}
}

func TestRunResearchFailsOnSourceError(t *testing.T) {
	src := wordstat.NewFake()
	src.Err = errors.New("mcp недоступен")
	fake := llm.NewFake()
	fake.Responses[agents.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}

	o := New(fake, researchOptions(src, Options{}))
	_, err := o.Run(context.Background(), researchBrief(), nil)
	if err == nil {
		t.Fatal("ожидалась ошибка прогона")
	}
	if !strings.Contains(err.Error(), "подбор тем") || !strings.Contains(err.Error(), "mcp недоступен") {
		t.Errorf("err = %v, want упоминание подбора и причины", err)
	}
	if fake.Calls[agents.RoleCopywriter] != 0 {
		t.Error("статьи не должны генерироваться после сбоя подбора")
	}
}

// Выдуманная цитата валит прогон: цифры должны быть проверяемыми.
func TestRunResearchFailsOnInventedCitation(t *testing.T) {
	src := winterSource()
	fake := llm.NewFake()
	fake.Responses[agents.RoleSeeds] = []string{`{"seeds":["зимняя резина"]}`}
	fake.Responses[agents.RoleCluster] = []string{`{"topics":[
		{"title":"Лучшая зимняя резина 2026","goal":"g","task":"t","queries":["лучшая зимняя резина 2026"]}]}`}

	o := New(fake, researchOptions(src, Options{}))
	_, err := o.Run(context.Background(), researchBrief(), nil)
	if err == nil {
		t.Fatal("ожидалась ошибка про неизвестный запрос")
	}
	if !errors.Is(err, agents.ErrUnknownQuery) {
		t.Errorf("errors.Is(ErrUnknownQuery) = false, err = %v", err)
	}
}

// Лимит обращений к Wordstat соблюдается, а прогресс не зависает на необработанных сеялках.
func TestRunResearchRespectsCallLimit(t *testing.T) {
	src := winterSource()
	fake := llm.NewFake()
	fake.Responses[agents.RoleSeeds] = []string{`{"seeds":["зимняя резина","какую зимнюю резину","какая зимняя резина лучше","купить зимнюю резину"]}`}
	fake.Responses[agents.RoleCluster] = []string{`{"topics":[
		{"title":"Как выбрать","goal":"g","task":"t","queries":["какую зимнюю резину"]}]}`}
	fake.Responses[agents.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"S","angle":"a","points":["x"]}]}`}
	fake.Responses[agents.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[agents.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	opt := researchOptions(src, Options{})
	opt.MaxWordstatCalls = 2
	o := New(fake, opt)

	p := newResearchProgress()
	if _, err := o.Run(context.Background(), researchBrief(), p); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := src.CallCount(); got != 2 {
		t.Errorf("обращений к Wordstat = %d, want 2 (лимит)", got)
	}
	if p.done != 4 {
		t.Errorf("закрытых сеялок = %d, want 4 (включая пропущенные по лимиту)", p.done)
	}
}

// Без настроенного источника подбор не запускается — работает прежний путь.
func TestRunWithoutWordstatSkipsResearch(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"T1","angle":"a","points":["x"]}]}`}
	fake.Responses[agents.RoleCopywriter] = []string{`{"topic":"t","title":"A","body":"b","cta":"c"}`}
	fake.Responses[agents.RoleCritic] = []string{`{"score":90,"issues":[],"verdict":"accept"}`}

	o := New(fake, Options{CriticMaxIter: 3, ScoreThreshold: 80})
	res, err := o.Run(context.Background(), brief(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Strategy.Topics) != 1 || res.Strategy.Topics[0].Title != "T1" {
		t.Errorf("темы = %+v, want от стратега", res.Strategy.Topics)
	}
	if fake.Calls[agents.RoleSeeds] != 0 || fake.Calls[agents.RoleCluster] != 0 {
		t.Error("семантика не должна вызываться без источника")
	}
}
