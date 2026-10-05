package topicservice

import (
	"context"
	"errors"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	"strings"
)

// Роли агента подбора тем. Идут на MODEL_DEFAULT: здесь важнее рассуждения,
// чем скорость.
const (
	RoleSeeds    = "semanticist_seeds"
	RoleCluster  = "semanticist_cluster"
	RoleSelect   = "semanticist_select"
	RoleFallback = "semanticist_fallback"
)

// DefaultSeedCount — сколько сеялок просим у модели, если не задано иное.
const DefaultSeedCount = 12

// ErrUnknownQuery — модель сослалась на запрос, которого нет в данных Wordstat.
// Это главная защита от выдуманных частотностей: цитаты обязаны быть реальными.
var ErrUnknownQuery = errors.New("select: запрос отсутствует в данных Wordstat")

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
func (s *Semanticist) Seeds(ctx context.Context, b topic.Briefing, count int) ([]string, corellm.Usage, error) {
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

const selectSystem = `Ты — редактор нативных статей. Тебе даны реальные поисковые фразы с
частотностями за последние 30 дней (показов в месяц) по продукту из брифа.

Собери из этих фраз темы статей и сам реши, какие темы идут в работу: порог объёма,
сезонность и релевантность оцениваешь ты, а не система.

Ответ строго в JSON:
{"topics": [{"title": "...", "goal": "...", "task": "...", "queries": ["..."],
             "intent": "...", "selected": true, "reject": "..."}]}

Правила:
- в queries указывай ТОЛЬКО фразы из переданного списка и дословно; фразы вне
  списка будут отброшены вместе с темой;
- selected: true — тему берём в работу; у остальных тем заполни reject короткой
  причиной без цифр (например «объём мал», «не наша аудитория», «технический
  запрос: размер или модель»);
- в работу нужно столько тем, сколько статей в задании; если подходящих меньше —
  отметь столько, сколько обоснованно, и объясни это в reject остальных;
- title — хук: понятная формулировка вопроса плюс обещание пользы
  (например, «Как выбрать зимние шины: 6 простых правил»);
- goal — кого и в какой момент мы ловим этой статьёй;
- task — что статья даёт читателю;
- intent — одно слово: вопрос, выбор, сравнение, инструкция или коммерческий;
- свои числа и проценты не приводи: частотности, объём и сезонность система
  подставит сама из данных Wordstat.`

// Select отдаёт модели сырые данные спроса (фразы с частотностями) и просит
// собрать темы и самой решить, какие идут в работу.
//
// Пороги, сезонность и релевантность — решение модели; код проверяет только то,
// что каждая цитата есть в данных (иначе ErrUnknownQuery) и что обязательные поля
// заполнены. Числа в промпте настоящие: их посчитал код по ответам Wordstat.
func (s *Semanticist) Select(ctx context.Context, b topic.Briefing, data []topic.PhraseCount, want int) ([]topic.TopicDraft, corellm.Usage, error) {
	if len(data) == 0 {
		return nil, corellm.Usage{}, fmt.Errorf("semanticist select: пустой список фраз")
	}
	if want <= 0 {
		want = 1
	}

	phrases := make([]string, 0, len(data))
	lines := make([]string, 0, len(data))
	for _, p := range data {
		phrases = append(phrases, p.Phrase)
		lines = append(lines, fmt.Sprintf("- %s — %d", p.Phrase, p.Count))
	}
	user := fmt.Sprintf(
		"Продукт: %s\nЦель: %s\nАудитория: %s\nТон: %s\n\nНужно статей: %d\n\nФразы из Wordstat (фраза — показов за 30 дней):\n%s",
		b.Product, b.Goal, b.Audience, b.Tone, want, strings.Join(lines, "\n"))

	var out struct {
		Topics []topic.TopicDraft `json:"topics"`
	}
	usage, err := s.llm.Complete(ctx, RoleSelect, selectSystem, user, &out)
	if err != nil {
		return nil, usage, fmt.Errorf("semanticist select: %w", err)
	}
	if len(out.Topics) == 0 {
		return nil, usage, fmt.Errorf("semanticist select: модель не вернула ни одной темы")
	}

	drafts, err := validateDrafts(out.Topics, phrases)
	if err != nil {
		return nil, usage, err
	}
	for i := range drafts {
		if drafts[i].Selected {
			drafts[i].Reject = "" // у выбранной темы причины отказа быть не может
		}
	}
	return drafts, usage, nil
}

// Cluster группирует собранные фразы в темы-кандидаты и проверяет, что каждая
// цитата действительно есть в данных (иначе — ErrUnknownQuery).
//
// Числа в промпт не передаются сознательно: модель не должна ни видеть
// частотности, ни тем более их придумывать.
func (s *Semanticist) Cluster(ctx context.Context, b topic.Briefing, phrases []string, wantTopics int) ([]topic.TopicDraft, corellm.Usage, error) {
	if len(phrases) == 0 {
		return nil, corellm.Usage{}, fmt.Errorf("semanticist cluster: пустой список фраз")
	}

	user := fmt.Sprintf(
		"Продукт: %s\nЦель: %s\nАудитория: %s\nТон: %s\n\nФразы из Wordstat:\n%s\n\nСобери %d тем.",
		b.Product, b.Goal, b.Audience, b.Tone, "- "+strings.Join(phrases, "\n- "), wantTopics)

	var out struct {
		Topics []topic.TopicDraft `json:"topics"`
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
func (s *Semanticist) Fallback(ctx context.Context, b topic.Briefing, want int, avoid []string) ([]topic.TopicDraft, corellm.Usage, error) {
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
		Topics []topic.TopicDraft `json:"topics"`
	}
	usage, err := s.llm.Complete(ctx, RoleFallback, fallbackSystem, user, &out)
	if err != nil {
		return nil, usage, fmt.Errorf("semanticist fallback: %w", err)
	}

	drafts := make([]topic.TopicDraft, 0, len(out.Topics))
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
func validateDrafts(drafts []topic.TopicDraft, phrases []string) ([]topic.TopicDraft, error) {
	index := make(map[string]string, len(phrases))
	for _, p := range phrases {
		index[strings.ToLower(strings.TrimSpace(p))] = strings.TrimSpace(p)
	}

	out := make([]topic.TopicDraft, 0, len(drafts))
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
