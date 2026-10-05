package topicservice_test

import (
	"context"
	"errors"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	"strings"
	"testing"

	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
)

// testBriefing — бриф в терминах подбора тем: topic не знает про campaign.Brief.
func testBriefing() topic.Briefing {
	return topic.Briefing{Product: "Эко-бутылка", Goal: "рост продаж", Audience: "ЗОЖ 25-40", Tone: "дружелюбный"}
}

func TestSemanticistSeeds(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{
		`{"seeds":["зимняя резина","  какую зимнюю резину  ","Зимняя Резина","","шины на зиму"]}`,
	}
	s := topicservice.NewSemanticist(fake)

	seeds, usage, err := s.Seeds(context.Background(), testBriefing(), 5)
	if err != nil {
		t.Fatalf("Seeds: %v", err)
	}
	// Пустые убраны, регистровый дубль схлопнут, пробелы обрезаны.
	want := []string{"зимняя резина", "какую зимнюю резину", "шины на зиму"}
	if len(seeds) != len(want) {
		t.Fatalf("seeds = %v, want %v", seeds, want)
	}
	for i := range want {
		if seeds[i] != want[i] {
			t.Errorf("seeds[%d] = %q, want %q", i, seeds[i], want[i])
		}
	}
	if usage.PromptTokens == 0 {
		t.Error("usage не зафиксирован")
	}

	req, ok := fake.LastRequest()
	if !ok {
		t.Fatal("нет записей о вызове LLM")
	}
	if req.Role != topicservice.RoleSeeds {
		t.Errorf("role = %q, want %q", req.Role, topicservice.RoleSeeds)
	}
	if !strings.Contains(req.User, "Эко-бутылка") || !strings.Contains(req.User, "5 поисковых фраз") {
		t.Errorf("в промпте нет брифа или количества фраз: %q", req.User)
	}
}

// Пустой ответ модели — это ошибка прогона: без сеялок искать спрос не по чему.
func TestSemanticistSeedsEmptyIsError(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSeeds] = []string{`{"seeds":[]}`}

	if _, _, err := topicservice.NewSemanticist(fake).Seeds(context.Background(), testBriefing(), 5); err == nil {
		t.Fatal("ожидалась ошибка на пустом списке сеялок")
	}
}

func TestSemanticistSeedsPropagatesLLMError(t *testing.T) {
	fake := mock.NewLLM()
	fake.Err = errors.New("llm недоступен")

	if _, _, err := topicservice.NewSemanticist(fake).Seeds(context.Background(), testBriefing(), 5); err == nil {
		t.Fatal("ожидалась ошибка LLM")
	}
}

func TestSemanticistClusterCanonicalizesCitations(t *testing.T) {
	phrases := []string{"зимняя резина", "какую зимнюю резину", "шины на зиму"}
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleCluster] = []string{`{"topics":[{
		"title":"Как выбрать зимние шины: 6 простых правил",
		"goal":"поймать аудиторию в момент выбора",
		"task":"дать чек-лист",
		"queries":["Какую Зимнюю Резину","зимняя резина","зимняя резина"],
		"intent":"Выбор"}]}`}

	drafts, usage, err := topicservice.NewSemanticist(fake).Cluster(context.Background(), testBriefing(), phrases, 2)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}
	if usage.PromptTokens == 0 {
		t.Error("usage не зафиксирован")
	}
	if len(drafts) != 1 {
		t.Fatalf("drafts = %d, want 1", len(drafts))
	}
	d := drafts[0]
	if d.Title == "" || d.Goal == "" || d.Task == "" {
		t.Errorf("тема без обязательных полей: %+v", d)
	}
	// Цитаты приведены к написанию из данных и дедуплицированы.
	wantQueries := []string{"какую зимнюю резину", "зимняя резина"}
	if len(d.Queries) != len(wantQueries) {
		t.Fatalf("queries = %v, want %v", d.Queries, wantQueries)
	}
	for i := range wantQueries {
		if d.Queries[i] != wantQueries[i] {
			t.Errorf("queries[%d] = %q, want %q", i, d.Queries[i], wantQueries[i])
		}
	}
	if d.Intent != "выбор" {
		t.Errorf("intent = %q, want выбор", d.Intent)
	}

	// Модель не должна видеть частотности: в промпт уходят только сами фразы.
	req, _ := fake.LastRequest()
	if !strings.Contains(req.User, "- зимняя резина\n- какую зимнюю резину\n- шины на зиму") {
		t.Errorf("список фраз ушёл в неожиданном виде: %q", req.User)
	}
	if strings.Contains(req.User, "1028481") || strings.Contains(req.User, "count") {
		t.Errorf("в промпт попали числа или служебные поля: %q", req.User)
	}
	if !strings.Contains(req.User, "Собери 2 тем") {
		t.Errorf("в промпте нет требуемого числа тем: %q", req.User)
	}
}

// Ключевая защита: выдуманная цитата — это ошибка, а не «почти правильно».
func TestSemanticistClusterRejectsUnknownQuery(t *testing.T) {
	phrases := []string{"зимняя резина", "шины на зиму"}
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleCluster] = []string{`{"topics":[{
		"title":"Как выбрать","goal":"g","task":"t",
		"queries":["зимняя резина","лучшая зимняя резина 2026"]}]}`}

	_, _, err := topicservice.NewSemanticist(fake).Cluster(context.Background(), testBriefing(), phrases, 1)
	if err == nil {
		t.Fatal("ожидалась ошибка про неизвестный запрос")
	}
	if !errors.Is(err, topicservice.ErrUnknownQuery) {
		t.Errorf("errors.Is(ErrUnknownQuery) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "лучшая зимняя резина 2026") {
		t.Errorf("в ошибке нет выдуманной фразы: %v", err)
	}
}

func TestSemanticistClusterRequiresFields(t *testing.T) {
	phrases := []string{"зимняя резина"}
	cases := map[string]string{
		"без title": `{"topics":[{"title":"","goal":"g","task":"t","queries":["зимняя резина"]}]}`,
		"без goal":  `{"topics":[{"title":"T","goal":"","task":"t","queries":["зимняя резина"]}]}`,
		"без task":  `{"topics":[{"title":"T","goal":"g","task":"","queries":["зимняя резина"]}]}`,
		"без фраз":  `{"topics":[{"title":"T","goal":"g","task":"t","queries":[]}]}`,
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			fake := mock.NewLLM()
			fake.Responses[topicservice.RoleCluster] = []string{resp}
			if _, _, err := topicservice.NewSemanticist(fake).Cluster(context.Background(), testBriefing(), phrases, 1); err == nil {
				t.Fatal("ожидалась ошибка валидации темы")
			}
		})
	}
}

func TestSemanticistClusterEmptyInputSkipsLLM(t *testing.T) {
	fake := mock.NewLLM()
	_, _, err := topicservice.NewSemanticist(fake).Cluster(context.Background(), testBriefing(), nil, 2)
	if err == nil {
		t.Fatal("ожидалась ошибка на пустом списке фраз")
	}
	if len(fake.Requests) != 0 {
		t.Error("без данных модель звать не нужно")
	}
}

func TestSemanticistClusterEmptyTopicsIsError(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleCluster] = []string{`{"topics":[]}`}
	if _, _, err := topicservice.NewSemanticist(fake).Cluster(context.Background(), testBriefing(), []string{"зимняя резина"}, 2); err == nil {
		t.Fatal("ожидалась ошибка на пустом списке тем")
	}
}

func TestSemanticistFallback(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleFallback] = []string{`{"topics":[
		{"title":"Как выбрать офис","goal":"поймать перед сделкой","task":"дать чек-лист","intent":"Выбор",
		 "queries":["выдуманный запрос"]},
		{"title":"","goal":"g","task":"t"}]}`}

	drafts, _, err := topicservice.NewSemanticist(fake).Fallback(context.Background(), testBriefing(), 2, []string{"Старая тема"})
	if err != nil {
		t.Fatalf("Fallback: %v", err)
	}
	// Тема без title отброшена, у оставшейся цитаты вычищены: данных нет.
	if len(drafts) != 1 {
		t.Fatalf("drafts = %d, want 1", len(drafts))
	}
	if len(drafts[0].Queries) != 0 {
		t.Errorf("у темы без данных не должно быть цитат: %v", drafts[0].Queries)
	}
	if drafts[0].Intent != "выбор" {
		t.Errorf("intent = %q, want выбор", drafts[0].Intent)
	}

	req, _ := fake.LastRequest()
	if req.Role != topicservice.RoleFallback {
		t.Errorf("role = %q, want %q", req.Role, topicservice.RoleFallback)
	}
	if !strings.Contains(req.User, "Старая тема") {
		t.Errorf("в промпте нет списка тем-исключений: %q", req.User)
	}
}

func TestSemanticistFallbackZeroWantSkipsLLM(t *testing.T) {
	fake := mock.NewLLM()
	drafts, _, err := topicservice.NewSemanticist(fake).Fallback(context.Background(), testBriefing(), 0, nil)
	if err != nil || drafts != nil {
		t.Fatalf("drafts = %v, err = %v; при want=0 модель звать не нужно", drafts, err)
	}
	if len(fake.Requests) != 0 {
		t.Error("LLM не должен вызываться")
	}
}

func TestSemanticistFallbackAllInvalidIsError(t *testing.T) {
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleFallback] = []string{`{"topics":[{"title":"T","goal":"","task":""}]}`}
	if _, _, err := topicservice.NewSemanticist(fake).Fallback(context.Background(), testBriefing(), 1, nil); err == nil {
		t.Fatal("ожидалась ошибка: пригодных тем нет")
	}
}

// Select: модель получает настоящие частотности и сама решает, что берём.
func TestSemanticistSelectShowsCountsAndKeepsDecision(t *testing.T) {
	data := []topic.PhraseCount{
		{Phrase: "зимняя резина", Count: 1028481},
		{Phrase: "какую зимнюю резину", Count: 92398},
		{Phrase: "шип", Count: 500000},
	}
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[
		{"title":"Как выбрать зимние шины","goal":"поймать в момент выбора","task":"дать чек-лист",
		 "queries":["какую зимнюю резину"],"intent":"выбор","selected":true},
		{"title":"Что такое шип","goal":"—","task":"—","queries":["шип"],
		 "selected":false,"reject":"не наша аудитория"}]}`}

	drafts, usage, err := topicservice.NewSemanticist(fake).Select(context.Background(), testBriefing(), data, 1)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if usage.PromptTokens == 0 {
		t.Error("usage не зафиксирован")
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts = %d, want 2", len(drafts))
	}
	if !drafts[0].Selected || drafts[0].Reject != "" {
		t.Errorf("выбранная тема: selected=%v reject=%q", drafts[0].Selected, drafts[0].Reject)
	}
	if drafts[1].Selected || drafts[1].Reject == "" {
		t.Errorf("отклонённая тема: selected=%v reject=%q", drafts[1].Selected, drafts[1].Reject)
	}

	// Числа уходят модели: без них решение о пороге невозможно.
	req, ok := fake.LastRequest()
	if !ok {
		t.Fatal("запрос не зафиксирован")
	}
	if req.Role != topicservice.RoleSelect {
		t.Errorf("роль = %q, want %q", req.Role, topicservice.RoleSelect)
	}
	for _, want := range []string{"зимняя резина — 1028481", "какую зимнюю резину — 92398", "Нужно статей: 1"} {
		if !strings.Contains(req.User, want) {
			t.Errorf("в промпте нет %q:\n%s", want, req.User)
		}
	}
}

// Цитаты по-прежнему обязаны быть из данных: выдуманная фраза валит подбор.
func TestSemanticistSelectRejectsUnknownQuery(t *testing.T) {
	data := []topic.PhraseCount{{Phrase: "зимняя резина", Count: 1000}}
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[{"title":"t","goal":"g","task":"k",
		"queries":["летняя резина"],"selected":true}]}`}

	_, _, err := topicservice.NewSemanticist(fake).Select(context.Background(), testBriefing(), data, 1)
	if !errors.Is(err, topicservice.ErrUnknownQuery) {
		t.Fatalf("err = %v, want ErrUnknownQuery", err)
	}
}

// Пустые данные — ошибка без обращения к модели.
func TestSemanticistSelectEmptyDataSkipsLLM(t *testing.T) {
	fake := mock.NewLLM()
	_, _, err := topicservice.NewSemanticist(fake).Select(context.Background(), testBriefing(), nil, 1)
	if err == nil {
		t.Fatal("ожидали ошибку на пустых данных")
	}
	if len(fake.Requests) != 0 {
		t.Errorf("вызовов модели = %d, want 0", len(fake.Requests))
	}
}

// Тема без обязательных полей — ошибка, а не тихо отброшенная запись.
func TestSemanticistSelectRequiresFields(t *testing.T) {
	data := []topic.PhraseCount{{Phrase: "зимняя резина", Count: 1000}}
	fake := mock.NewLLM()
	fake.Responses[topicservice.RoleSelect] = []string{`{"topics":[{"title":"","goal":"g","task":"k","queries":["зимняя резина"]}]}`}
	if _, _, err := topicservice.NewSemanticist(fake).Select(context.Background(), testBriefing(), data, 1); err == nil {
		t.Fatal("ожидали ошибку про незаполненные поля")
	}
}
