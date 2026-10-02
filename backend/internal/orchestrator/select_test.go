package orchestrator

import (
	"testing"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
)

// Числа в тестах — из живых фикстур Wordstat (backend/internal/wordstat/testdata):
// «зимняя резина» 1 028 481 за 30 дней, «какую зимнюю резину» 92 398,
// «купить зимнюю резину» 289 429, сумма 50 формулировок — 3 487 006.
var selectOpts = SelectOptions{MinVolume: 300, SeasonalityFactor: 3}

func draft(title, goal string) agents.TopicDraft {
	return agents.TopicDraft{Title: title, Goal: goal, Task: "задача"}
}

func winterDynamics() []wordstat.DynamicsPoint {
	// Реальные точки dynamics за 12 месяцев: пик в октябре, дно в июне.
	return []wordstat.DynamicsPoint{
		{Date: "2025-10-01T00:00:00Z", Count: 1700930},
		{Date: "2025-11-01T00:00:00Z", Count: 1228062},
		{Date: "2026-02-01T00:00:00Z", Count: 212236},
		{Date: "2026-06-01T00:00:00Z", Count: 192109},
		{Date: "2026-09-01T00:00:00Z", Count: 1027620},
	}
}

// Объём темы — максимум по цитатам, а не сумма: сумма завысила бы спрос в 3+ раза.
func TestSelectTopicsVolumeIsMaxNotSum(t *testing.T) {
	inputs := []DraftInput{{
		Draft: draft("Как выбрать зимние шины", "поймать в момент выбора"),
		Queries: []agents.PhraseCount{
			{Phrase: "зимняя резина", Count: 1028481},
			{Phrase: "купить зимнюю резину", Count: 289429},
			{Phrase: "какую зимнюю резину", Count: 92398},
		},
	}}

	cands := SelectTopics(inputs, 1, selectOpts)
	if len(cands) != 1 {
		t.Fatalf("кандидатов %d, want 1", len(cands))
	}
	if cands[0].Volume != 1028481 {
		t.Errorf("Volume = %d, want 1028481 (максимум, а не 1 410 308)", cands[0].Volume)
	}
	if cands[0].Head != "зимняя резина" {
		t.Errorf("Head = %q, want «зимняя резина»", cands[0].Head)
	}
	if !cands[0].Selected {
		t.Error("тема должна быть отобрана")
	}
}

// Размеры и типоразмеры — не темы, даже если спрос по ним большой.
func TestSelectTopicsDropsTechnicalTopics(t *testing.T) {
	inputs := []DraftInput{
		{Draft: draft("Зимняя резина 205 55 16", "размер"), Queries: []agents.PhraseCount{
			{Phrase: "зимняя резина 205 55 16", Count: 16880},
			{Phrase: "зимняя резина r16", Count: 35202},
		}},
		{Draft: draft("Какую зимнюю резину выбрать", "выбор"), Queries: []agents.PhraseCount{
			{Phrase: "какую зимнюю резину", Count: 92398},
		}},
	}

	cands := SelectTopics(inputs, 2, selectOpts)
	if len(cands) != 2 {
		t.Fatalf("кандидатов %d, want 2 (отклонённые остаются с причиной)", len(cands))
	}
	for _, c := range cands {
		if c.Head == "зимняя резина r16" {
			if c.Reject != rejectTechnical {
				t.Errorf("техническая тема не помечена: %+v", c)
			}
			if c.Selected {
				t.Error("техническая тема не должна идти в генерацию")
			}
		}
	}
	if !cands[0].Selected || cands[0].Head != "какую зимнюю резину" {
		t.Errorf("ожидалась отобранной тема про выбор, получили %+v", cands[0])
	}
}

// Порог: тема ниже минимума не идёт в генерацию, но видна с причиной.
func TestSelectTopicsMinVolume(t *testing.T) {
	inputs := []DraftInput{
		{Draft: draft("Сильная тема", "g"), Queries: []agents.PhraseCount{{Phrase: "какую зимнюю резину", Count: 92398}}},
		{Draft: draft("Слабая тема", "g"), Queries: []agents.PhraseCount{{Phrase: "резина для квадроцикла зима", Count: 120}}},
	}

	cands := SelectTopics(inputs, 2, selectOpts)
	var weak *agents.TopicCandidate
	for i := range cands {
		if cands[i].Title == "Слабая тема" {
			weak = &cands[i]
		}
	}
	if weak == nil {
		t.Fatal("слабая тема потерялась из результата")
	}
	if weak.Reject != rejectLowVolume || weak.Selected {
		t.Errorf("слабая тема: reject = %q, selected = %v", weak.Reject, weak.Selected)
	}
}

// Сезонная тема: спрос в межсезонье ниже порога, но пик за 12 месяцев — выше,
// и размах больше множителя. Порог не должен её убивать.
func TestSelectTopicsSeasonalRescue(t *testing.T) {
	season := SeasonalityOf(winterDynamics(), selectOpts.SeasonalityFactor)
	if season == nil || !season.Seasonal {
		t.Fatalf("сезонность не распознана: %+v", season)
	}
	if season.Peak != 1700930 || season.PeakMonth != "2025-10" {
		t.Errorf("пик = %d (%s), want 1700930 (2025-10)", season.Peak, season.PeakMonth)
	}
	if season.Ratio < 8 || season.Ratio > 9 {
		t.Errorf("размах = %.2f, want ≈8.85", season.Ratio)
	}

	inputs := []DraftInput{{
		Draft:   draft("Когда менять резину на зимнюю", "сезонный вопрос"),
		Queries: []agents.PhraseCount{{Phrase: "какую зимнюю резину", Count: 200}},
		Season:  season,
	}}

	cands := SelectTopics(inputs, 1, selectOpts)
	if len(cands) != 1 {
		t.Fatalf("кандидатов %d, want 1", len(cands))
	}
	if cands[0].Reject != "" {
		t.Errorf("сезонная тема отклонена: %q", cands[0].Reject)
	}
	if !cands[0].Selected {
		t.Error("сезонная тема должна быть отобрана")
	}
	// Объём остаётся фактическим (окно 30 дней), а для сортировки берётся пик.
	if cands[0].Volume != 200 {
		t.Errorf("Volume = %d, want 200 (фактический спрос окна)", cands[0].Volume)
	}
	if rankVolume(cands[0]) != 1700930 {
		t.Errorf("rankVolume = %d, want пик 1700930", rankVolume(cands[0]))
	}
}

// Сезонность без большого размаха — не спасение: тема остаётся ниже порога.
func TestSelectTopicsFlatSeasonalityDoesNotRescue(t *testing.T) {
	flat := SeasonalityOf([]wordstat.DynamicsPoint{
		{Date: "2026-01-01T00:00:00Z", Count: 500},
		{Date: "2026-02-01T00:00:00Z", Count: 450},
	}, selectOpts.SeasonalityFactor)
	inputs := []DraftInput{{
		Draft:   draft("Ровный спрос", "g"),
		Queries: []agents.PhraseCount{{Phrase: "офисная мебель для переговорной", Count: 100}},
		Season:  flat,
	}}

	cands := SelectTopics(inputs, 1, selectOpts)
	if cands[0].Reject != rejectLowVolume {
		t.Errorf("reject = %q, want %q", cands[0].Reject, rejectLowVolume)
	}
}

// Отбор N из 2N: сверху по объёму, отобранные идут первыми.
func TestSelectTopicsPicksTopNOfDouble(t *testing.T) {
	inputs := []DraftInput{
		{Draft: draft("Тема A", "g"), Queries: []agents.PhraseCount{{Phrase: "a", Count: 1000}}},
		{Draft: draft("Тема B", "g"), Queries: []agents.PhraseCount{{Phrase: "b", Count: 5000}}},
		{Draft: draft("Тема C", "g"), Queries: []agents.PhraseCount{{Phrase: "c", Count: 3000}}},
		{Draft: draft("Тема D", "g"), Queries: []agents.PhraseCount{{Phrase: "d", Count: 400}}},
		{Draft: draft("Тема E", "g"), Queries: []agents.PhraseCount{{Phrase: "e", Count: 9000}}},
		{Draft: draft("Тема F", "g"), Queries: []agents.PhraseCount{{Phrase: "f", Count: 2000}}},
	}

	cands := SelectTopics(inputs, 3, selectOpts)
	if len(cands) != 6 {
		t.Fatalf("кандидатов %d, want 6 (все рассмотренные)", len(cands))
	}

	var selected []string
	for _, c := range cands {
		if c.Selected {
			selected = append(selected, c.Title)
		}
	}
	wantSelected := []string{"Тема E", "Тема B", "Тема C"}
	if len(selected) != len(wantSelected) {
		t.Fatalf("отобрано %v, want %v", selected, wantSelected)
	}
	for i := range wantSelected {
		if selected[i] != wantSelected[i] {
			t.Errorf("selected[%d] = %q, want %q", i, selected[i], wantSelected[i])
		}
	}
	if !cands[0].Selected || !cands[1].Selected || !cands[2].Selected {
		t.Error("отобранные темы должны идти первыми")
	}
}

// Fallback: подтверждённых тем не хватило — добираем темами от модели, без цифр.
func TestSelectTopicsFallbackFillsGap(t *testing.T) {
	inputs := []DraftInput{{
		Draft:   draft("С подтверждённым спросом", "g"),
		Queries: []agents.PhraseCount{{Phrase: "какую зимнюю резину", Count: 92398}},
	}}
	inputs = append(inputs, FallbackInputs([]agents.TopicDraft{
		draft("Тема от модели 1", "g"),
		draft("Тема от модели 2", "g"),
	})...)

	cands := SelectTopics(inputs, 2, selectOpts)
	var selected []agents.TopicCandidate
	for _, c := range cands {
		if c.Selected {
			selected = append(selected, c)
		}
	}
	if len(selected) != 2 {
		t.Fatalf("отобрано %d тем, want 2", len(selected))
	}
	if selected[0].Source != agents.SourceWordstat {
		t.Errorf("первой должна идти тема с данными, получили %q", selected[0].Source)
	}
	if selected[1].Source != agents.SourceLLM {
		t.Fatalf("второй должна идти тема от модели, получили %q", selected[1].Source)
	}
	if selected[1].Volume != 0 || len(selected[1].Queries) != 0 {
		t.Errorf("у темы без данных не должно быть цифр: %+v", selected[1])
	}
	if selected[1].Reject != "" {
		t.Errorf("тема от модели не отклоняется порогом: %q", selected[1].Reject)
	}
}

// Когда данных хватает, темы от модели остаются в списке, но не отбираются.
func TestSelectTopicsFallbackNotUsedWhenEnoughData(t *testing.T) {
	inputs := []DraftInput{
		{Draft: draft("A", "g"), Queries: []agents.PhraseCount{{Phrase: "a", Count: 5000}}},
		{Draft: draft("B", "g"), Queries: []agents.PhraseCount{{Phrase: "b", Count: 4000}}},
	}
	inputs = append(inputs, FallbackInputs([]agents.TopicDraft{draft("Модель", "g")})...)

	cands := SelectTopics(inputs, 2, selectOpts)
	for _, c := range cands {
		if c.Source == agents.SourceLLM && c.Selected {
			t.Error("тема от модели не должна отбираться, когда данных хватает")
		}
	}
}

func TestSelectedTopicsMapsToPipelineTopics(t *testing.T) {
	inputs := []DraftInput{{
		Draft: draft("Как выбрать зимние шины: 6 простых правил", "поймать в момент выбора"),
		Queries: []agents.PhraseCount{
			{Phrase: "какую зимнюю резину", Count: 92398},
			{Phrase: "какая зимняя резина лучше", Count: 34038},
		},
	}}

	topics := SelectedTopics(SelectTopics(inputs, 1, selectOpts))
	if len(topics) != 1 {
		t.Fatalf("тем %d, want 1", len(topics))
	}
	tp := topics[0]
	if tp.Title != "Как выбрать зимние шины: 6 простых правил" {
		t.Errorf("Title = %q", tp.Title)
	}
	if tp.Angle != "поймать в момент выбора" {
		t.Errorf("Angle = %q, want цель темы", tp.Angle)
	}
	wantPoints := []string{"какую зимнюю резину", "какая зимняя резина лучше"}
	if len(tp.Points) != len(wantPoints) {
		t.Fatalf("Points = %v, want %v", tp.Points, wantPoints)
	}
	for i := range wantPoints {
		if tp.Points[i] != wantPoints[i] {
			t.Errorf("Points[%d] = %q, want %q", i, tp.Points[i], wantPoints[i])
		}
	}
}

func TestIntentOf(t *testing.T) {
	cases := map[string]string{
		"какую зимнюю резину":       "вопрос",
		"как выбрать зимние шины":   "вопрос",
		"какая зимняя резина лучше": "вопрос",
		"лучшая зимняя резина":      "сравнение",
		"рейтинг зимней резины":     "сравнение",
		"купить зимнюю резину":      "коммерческий",
		"зимняя резина цена":        "коммерческий",
		"зимняя резина":             "",
		"зимняя резина 205 55 16":   "",
		"подобрать резину на зиму":  "выбор",
	}
	for phrase, want := range cases {
		if got := IntentOf(phrase); got != want {
			t.Errorf("IntentOf(%q) = %q, want %q", phrase, got, want)
		}
	}
}

func TestSeasonalityOfEmptyPoints(t *testing.T) {
	if s := SeasonalityOf(nil, 3); s != nil {
		t.Errorf("без точек сезонности быть не может: %+v", s)
	}
}
