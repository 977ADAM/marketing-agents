package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/llm"
)

func TestSemanticistSeeds(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[RoleSeeds] = []string{
		`{"seeds":["зимняя резина","  какую зимнюю резину  ","Зимняя Резина","","шины на зиму"]}`,
	}
	s := NewSemanticist(fake)

	seeds, usage, err := s.Seeds(context.Background(), testBrief(), 5)
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
	if req.Role != RoleSeeds {
		t.Errorf("role = %q, want %q", req.Role, RoleSeeds)
	}
	if !strings.Contains(req.User, "Эко-бутылка") || !strings.Contains(req.User, "5 поисковых фраз") {
		t.Errorf("в промпте нет брифа или количества фраз: %q", req.User)
	}
}

// Пустой ответ модели — это ошибка прогона: без сеялок искать спрос не по чему.
func TestSemanticistSeedsEmptyIsError(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[RoleSeeds] = []string{`{"seeds":[]}`}

	if _, _, err := NewSemanticist(fake).Seeds(context.Background(), testBrief(), 5); err == nil {
		t.Fatal("ожидалась ошибка на пустом списке сеялок")
	}
}

func TestSemanticistSeedsPropagatesLLMError(t *testing.T) {
	fake := llm.NewFake()
	fake.Err = errors.New("llm недоступен")

	if _, _, err := NewSemanticist(fake).Seeds(context.Background(), testBrief(), 5); err == nil {
		t.Fatal("ожидалась ошибка LLM")
	}
}

func TestSemanticistClusterCanonicalizesCitations(t *testing.T) {
	phrases := []string{"зимняя резина", "какую зимнюю резину", "шины на зиму"}
	fake := llm.NewFake()
	fake.Responses[RoleCluster] = []string{`{"topics":[{
		"title":"Как выбрать зимние шины: 6 простых правил",
		"goal":"поймать аудиторию в момент выбора",
		"task":"дать чек-лист",
		"queries":["Какую Зимнюю Резину","зимняя резина","зимняя резина"],
		"intent":"Выбор"}]}`}

	drafts, usage, err := NewSemanticist(fake).Cluster(context.Background(), testBrief(), phrases, 2)
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
	fake := llm.NewFake()
	fake.Responses[RoleCluster] = []string{`{"topics":[{
		"title":"Как выбрать","goal":"g","task":"t",
		"queries":["зимняя резина","лучшая зимняя резина 2026"]}]}`}

	_, _, err := NewSemanticist(fake).Cluster(context.Background(), testBrief(), phrases, 1)
	if err == nil {
		t.Fatal("ожидалась ошибка про неизвестный запрос")
	}
	if !errors.Is(err, ErrUnknownQuery) {
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
			fake := llm.NewFake()
			fake.Responses[RoleCluster] = []string{resp}
			if _, _, err := NewSemanticist(fake).Cluster(context.Background(), testBrief(), phrases, 1); err == nil {
				t.Fatal("ожидалась ошибка валидации темы")
			}
		})
	}
}

func TestSemanticistClusterEmptyInputSkipsLLM(t *testing.T) {
	fake := llm.NewFake()
	_, _, err := NewSemanticist(fake).Cluster(context.Background(), testBrief(), nil, 2)
	if err == nil {
		t.Fatal("ожидалась ошибка на пустом списке фраз")
	}
	if len(fake.Requests) != 0 {
		t.Error("без данных модель звать не нужно")
	}
}

func TestSemanticistClusterEmptyTopicsIsError(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[RoleCluster] = []string{`{"topics":[]}`}
	if _, _, err := NewSemanticist(fake).Cluster(context.Background(), testBrief(), []string{"зимняя резина"}, 2); err == nil {
		t.Fatal("ожидалась ошибка на пустом списке тем")
	}
}
