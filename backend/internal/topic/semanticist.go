package topic

import (
	"context"
	"errors"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/corellm"
	"strings"
)

// Роли агента подбора тем. Идут на MODEL_DEFAULT: здесь важнее рассуждения,
// чем скорость.
const (
	RoleSeeds    = "semanticist_seeds"
	RoleCluster  = "semanticist_cluster"
	RoleFallback = "semanticist_fallback"
)

// DefaultSeedCount — сколько сеялок просим у модели, если не задано иное.
const DefaultSeedCount = 12

// ErrUnknownQuery — модель сослалась на запрос, которого нет в данных Wordstat.
// Это главная защита от выдуманных частотностей: цитаты обязаны быть реальными.
var ErrUnknownQuery = errors.New("cluster: запрос отсутствует в данных Wordstat")

// Semanticist — агент, который предлагает темы на основе поискового спроса:
// сначала сеялки по брифу, затем группировка уже собранных фраз в темы.
type Semanticist struct{ llm corellm.Client }

func NewSemanticist(c corellm.Client) *Semanticist { return &Semanticist{llm: c} }

const seedsSystem = `Ты — маркетинговый аналитик. По брифу выпиши короткие поисковые фразы,
которые реально вводят люди в Яндекс, когда ищут такой продукт или решают такую задачу.
Ответ строго в JSON: {"seeds": ["...", "..."]}.
Требования к фразам: 2–4 слова; так, как пишут в поиске, а не как в рекламе;
без названий компаний и брендов; без знаков препинания; всё в нижнем регистре.
Фразы должны покрывать разные стороны продукта (выбор, применение, цена, сравнение),
а не быть пересказом одной мысли.`

const clusterSystem = `Ты — редактор, который собирает темы нативных статей на основе поискового спроса.
Тебе дан список реальных поисковых фраз по продукту. Сгруппируй их в темы статей.
Ответ строго в JSON:
{"topics": [{"title": "...", "goal": "...", "task": "...", "queries": ["..."], "intent": "..."}]}.
Правила:
- в queries указывай ТОЛЬКО фразы из переданного списка и дословно; ничего не придумывай
  и не переформулируй — фразы вне списка будут отброшены вместе с темой;
- title — хук: понятная формулировка вопроса плюс обещание пользы
  (например, «Как выбрать зимние шины: 6 простых правил»);
- goal — кого и в какой момент мы ловим этой статьёй;
- task — что статья даёт читателю;
- intent — одно слово: вопрос, выбор, сравнение, инструкция или коммерческий;
- числа, частотности и проценты не приводи: их подставит система.`

// Seeds просит у модели 10–15 поисковых фраз по брифу.
func (s *Semanticist) Seeds(ctx context.Context, b Briefing, count int) ([]string, corellm.Usage, error) {
	if count <= 0 {
		count = DefaultSeedCount
	}
	user := fmt.Sprintf(
		"Продукт: %s\nЦель: %s\nАудитория: %s\nТон: %s\n\nВыпиши %d поисковых фраз.",
		b.Product, b.Goal, b.Audience, b.Tone, count)

	var out struct {
		Seeds []string `json:"seeds"`
	}
	usage, err := s.llm.Complete(ctx, RoleSeeds, seedsSystem, user, &out)
	if err != nil {
		return nil, usage, fmt.Errorf("semanticist seeds: %w", err)
	}

	seeds := cleanSeeds(out.Seeds)
	if len(seeds) == 0 {
		return nil, usage, fmt.Errorf("semanticist seeds: модель не вернула ни одной фразы")
	}
	return seeds, usage, nil
}

// Cluster группирует собранные фразы в темы-кандидаты и проверяет, что каждая
// цитата действительно есть в данных (иначе — ErrUnknownQuery).
//
// Числа в промпт не передаются сознательно: модель не должна ни видеть
// частотности, ни тем более их придумывать.
func (s *Semanticist) Cluster(ctx context.Context, b Briefing, phrases []string, wantTopics int) ([]TopicDraft, corellm.Usage, error) {
	if len(phrases) == 0 {
		return nil, corellm.Usage{}, fmt.Errorf("semanticist cluster: пустой список фраз")
	}

	user := fmt.Sprintf(
		"Продукт: %s\nЦель: %s\nАудитория: %s\nТон: %s\n\nФразы из Wordstat:\n%s\n\nСобери %d тем.",
		b.Product, b.Goal, b.Audience, b.Tone, "- "+strings.Join(phrases, "\n- "), wantTopics)

	var out struct {
		Topics []TopicDraft `json:"topics"`
	}
	usage, err := s.llm.Complete(ctx, RoleCluster, clusterSystem, user, &out)
	if err != nil {
		return nil, usage, fmt.Errorf("semanticist cluster: %w", err)
	}
	if len(out.Topics) == 0 {
		return nil, usage, fmt.Errorf("semanticist cluster: модель не вернула ни одной темы")
	}

	drafts, err := validateDrafts(out.Topics, phrases)
	if err != nil {
		return nil, usage, err
	}
	return drafts, usage, nil
}

// Fallback просит темы «от себя», когда спроса нет или его не хватило на
// нужное число тем: строго по ЦА, продукту и задаче из брифа. Такие темы
// помечаются источником llm и в отчёте идут без цифр — это гипотеза, а не данные.
func (s *Semanticist) Fallback(ctx context.Context, b Briefing, want int, avoid []string) ([]TopicDraft, corellm.Usage, error) {
	if want <= 0 {
		return nil, corellm.Usage{}, nil
	}
	avoidNote := ""
	if len(avoid) > 0 {
		avoidNote = "\nНе повторяй эти темы: " + strings.Join(avoid, "; ") + "."
	}
	user := fmt.Sprintf(
		"Продукт: %s\nЦель: %s\nАудитория: %s\nТон: %s\n\nПредложи %d тем для нативных статей.%s",
		b.Product, b.Goal, b.Audience, b.Tone, want, avoidNote)

	var out struct {
		Topics []TopicDraft `json:"topics"`
	}
	usage, err := s.llm.Complete(ctx, RoleFallback, fallbackSystem, user, &out)
	if err != nil {
		return nil, usage, fmt.Errorf("semanticist fallback: %w", err)
	}

	drafts := make([]TopicDraft, 0, len(out.Topics))
	for _, d := range out.Topics {
		d.Title = strings.TrimSpace(d.Title)
		d.Goal = strings.TrimSpace(d.Goal)
		d.Task = strings.TrimSpace(d.Task)
		d.Intent = strings.ToLower(strings.TrimSpace(d.Intent))
		d.Queries = nil // у тем без данных цитат быть не может
		if d.Title == "" || d.Goal == "" || d.Task == "" {
			continue
		}
		drafts = append(drafts, d)
	}
	if len(drafts) == 0 {
		return nil, usage, fmt.Errorf("semanticist fallback: модель не вернула пригодных тем")
	}
	return drafts, usage, nil
}

const fallbackSystem = `Ты — редактор нативных статей. Поисковых данных по продукту нет или их мало,
поэтому предложи темы сам — строго по продукту, целевой аудитории и задаче из брифа.
Ответ строго в JSON:
{"topics": [{"title": "...", "goal": "...", "task": "...", "intent": "..."}]}.
Правила:
- title — хук: понятная формулировка вопроса плюс обещание пользы;
- goal — кого и в какой момент мы ловим этой статьёй;
- task — что статья даёт читателю;
- intent — одно слово: вопрос, выбор, сравнение, инструкция или коммерческий;
- никаких чисел, частотностей и процентов: данных нет, выдумывать их нельзя.`

// cleanSeeds приводит сеялки к единому виду: обрезает пробелы, убирает пустые и
// повторы, сохраняя порядок.
func cleanSeeds(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// validateDrafts проверяет темы и канонизирует цитаты: каждая фраза обязана
// найтись в наборе (сравнение без учёта регистра), а в результат попадает
// написание из данных, а не из ответа модели.
func validateDrafts(drafts []TopicDraft, phrases []string) ([]TopicDraft, error) {
	index := make(map[string]string, len(phrases))
	for _, p := range phrases {
		index[strings.ToLower(strings.TrimSpace(p))] = strings.TrimSpace(p)
	}

	out := make([]TopicDraft, 0, len(drafts))
	for _, d := range drafts {
		d.Title = strings.TrimSpace(d.Title)
		d.Goal = strings.TrimSpace(d.Goal)
		d.Task = strings.TrimSpace(d.Task)
		d.Intent = strings.ToLower(strings.TrimSpace(d.Intent))

		if d.Title == "" || d.Goal == "" || d.Task == "" {
			return nil, fmt.Errorf("semanticist cluster: тема %q без title/goal/task", d.Title)
		}
		if len(d.Queries) == 0 {
			return nil, fmt.Errorf("semanticist cluster: тема %q без запросов", d.Title)
		}

		seen := make(map[string]bool, len(d.Queries))
		queries := make([]string, 0, len(d.Queries))
		for _, q := range d.Queries {
			canonical, ok := index[strings.ToLower(strings.TrimSpace(q))]
			if !ok {
				return nil, fmt.Errorf("%w: %q (тема %q)", ErrUnknownQuery, q, d.Title)
			}
			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			queries = append(queries, canonical)
		}
		d.Queries = queries
		out = append(out, d)
	}
	return out, nil
}
