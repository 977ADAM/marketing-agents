package orchestrator

import (
	"context"
	"fmt"
	"sort"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
)

// Значения по умолчанию для подбора тем (переопределяются через Options).
const (
	// DefaultTopicsCount — сколько статей считаем нормой, если бриф не задал число.
	DefaultTopicsCount = 3
	// DefaultNumPhrases — сколько фраз просить у Wordstat на одну сеялку.
	DefaultNumPhrases = 50
	// DefaultMaxWordstatCalls — лимит обращений к Wordstat на прогон.
	DefaultMaxWordstatCalls = 60
	// DefaultMaxPhrases — сколько фраз отдавать модели на кластеризацию.
	DefaultMaxPhrases = 40
)

// research собирает темы на поисковом спросе: сеялки → спрос → кластеры → отбор.
//
// Числа считает только этот код: модель их не видит и не возвращает, а каждая её
// цитата проверена по данным (см. agents.Semanticist). Если спроса нет совсем или
// подтверждённых тем не хватило, добираем темы от модели с пометкой source=llm —
// без цифр, потому что цифр по ним нет.
func (o *Orchestrator) research(ctx context.Context, b agents.Brief, p Progress) (agents.Strategy, llm.Usage, error) {
	var total llm.Usage
	rp, hasRP := p.(ResearchProgress)
	stage := func(s ResearchStage) {
		if hasRP {
			rp.Researching(s)
		}
	}

	want := b.TopicsCount
	if want <= 0 {
		want = DefaultTopicsCount
	}
	mult := o.opt.TopicsMultiplier
	if mult < 1 {
		mult = 1
	}
	maxCalls := o.maxWordstatCalls()
	regions := regionList(b.Region, o.opt.DefaultRegion)
	calls := 0

	// 1) Сеялки по брифу.
	stage(StageSeeds)
	seeds, u, err := o.semanticist.Seeds(ctx, b, o.seedCount())
	total = total.Add(u)
	if err != nil {
		return agents.Strategy{}, total, fmt.Errorf("подбор тем: %w", err)
	}
	if hasRP {
		rp.ResearchSeeds(seeds)
	}

	// 2) Спрос по каждой сеялке.
	stage(StageFetching)
	counts := map[string]int64{}
	processed := 0
	for i, seed := range seeds {
		if calls >= maxCalls {
			break
		}
		top, err := o.opt.Wordstat.TopRequests(ctx, wordstat.TopParams{
			Phrase:     seed,
			NumPhrases: o.numPhrases(),
			Regions:    regions,
		})
		calls++
		if err != nil {
			return agents.Strategy{}, total, fmt.Errorf("подбор тем: спрос по %q: %w", seed, err)
		}
		collectCounts(counts, top)
		processed = i + 1
		if hasRP {
			rp.ResearchSeedDone(i)
		}
	}
	// Сеялки, до которых не дошла очередь из-за лимита, тоже закрываем —
	// иначе прогресс останется висеть на них.
	if hasRP {
		for i := processed; i < len(seeds); i++ {
			rp.ResearchSeedDone(i)
		}
	}

	// 3) Кластеризация: модели отдаём только сами фразы, без частотностей и без
	// технического мусора (размеры и типоразмеры — не темы).
	var drafts []agents.TopicDraft
	if phrases := topPhrases(counts, o.maxPhrases()); len(phrases) > 0 {
		stage(StageClustering)
		drafts, u, err = o.semanticist.Cluster(ctx, b, phrases, want*mult)
		total = total.Add(u)
		if err != nil {
			return agents.Strategy{}, total, fmt.Errorf("подбор тем: %w", err)
		}
	}

	// 4) Сезонная поправка по головной фразе каждой темы.
	stage(StageSelecting)
	inputs := make([]DraftInput, 0, len(drafts))
	for _, d := range drafts {
		queries := queriesOf(d.Queries, counts)
		var season *agents.Seasonality
		if head := headOf(queries); head != "" && calls < maxCalls {
			dyn, err := o.opt.Wordstat.Dynamics(ctx, wordstat.DynamicsParams{
				Phrase:  head,
				Period:  "monthly",
				Regions: regions,
			})
			calls++
			if err == nil {
				season = SeasonalityOf(dyn.Points, o.opt.Select.SeasonalityFactor)
			}
			// Сезонность — обогащение, а не обязательные данные: её сбой не валит
			// подбор, тема просто оценивается по текущему окну.
		}
		inputs = append(inputs, DraftInput{
			Draft:   d,
			Source:  agents.SourceWordstat,
			Queries: queries,
			Season:  season,
		})
	}

	cands := SelectTopics(inputs, want, o.opt.Select)

	// 5) Fallback: спроса нет или подтверждённых тем не хватило.
	if selected := countSelected(cands); selected < want {
		fallback, u, err := o.semanticist.Fallback(ctx, b, want-selected, titlesOf(cands))
		total = total.Add(u)
		switch {
		case err != nil && selected == 0:
			return agents.Strategy{}, total, fmt.Errorf("подбор тем: %w", err)
		case err == nil:
			cands = SelectTopics(append(inputs, FallbackInputs(fallback)...), want, o.opt.Select)
		}
	}

	if countSelected(cands) == 0 {
		return agents.Strategy{}, total, fmt.Errorf("подбор тем: не удалось собрать ни одной темы")
	}

	return agents.Strategy{
		Topics:          SelectedTopics(cands),
		TopicCandidates: cands,
		WordstatCalls:   calls,
	}, total, nil
}

func (o *Orchestrator) seedCount() int {
	if o.opt.SeedCount > 0 {
		return o.opt.SeedCount
	}
	return agents.DefaultSeedCount
}

func (o *Orchestrator) numPhrases() int {
	if o.opt.NumPhrases > 0 {
		return o.opt.NumPhrases
	}
	return DefaultNumPhrases
}

func (o *Orchestrator) maxPhrases() int {
	if o.opt.MaxPhrases > 0 {
		return o.opt.MaxPhrases
	}
	return DefaultMaxPhrases
}

func (o *Orchestrator) maxWordstatCalls() int {
	if o.opt.MaxWordstatCalls > 0 {
		return o.opt.MaxWordstatCalls
	}
	return DefaultMaxWordstatCalls
}

// regionList собирает фильтр регионов: из брифа, иначе регион по умолчанию.
func regionList(briefRegion, defaultRegion string) []string {
	region := briefRegion
	if region == "" {
		region = defaultRegion
	}
	if region == "" {
		return nil
	}
	return []string{region}
}

// collectCounts складывает фразы сеялки в общий словарь «фраза → частотность».
// Одинаковые формулировки из разных сеялок схлопываются по максимуму.
func collectCounts(counts map[string]int64, top *wordstat.Top) {
	for _, q := range append(append([]wordstat.PhraseCount{}, top.Requests...), top.Associations...) {
		if q.Count > counts[q.Phrase] {
			counts[q.Phrase] = q.Count
		}
	}
}

// topPhrases возвращает фразы для кластеризации: без технического мусора, по
// убыванию частотности, не больше limit.
func topPhrases(counts map[string]int64, limit int) []string {
	phrases := make([]string, 0, len(counts))
	for phrase := range counts {
		if isTechnical(phrase) {
			continue
		}
		phrases = append(phrases, phrase)
	}
	sort.SliceStable(phrases, func(i, j int) bool {
		if counts[phrases[i]] != counts[phrases[j]] {
			return counts[phrases[i]] > counts[phrases[j]]
		}
		return phrases[i] < phrases[j]
	})
	if limit > 0 && len(phrases) > limit {
		phrases = phrases[:limit]
	}
	return phrases
}

// queriesOf превращает цитаты темы в пары «фраза → частотность».
func queriesOf(phrases []string, counts map[string]int64) []agents.PhraseCount {
	out := make([]agents.PhraseCount, 0, len(phrases))
	for _, p := range phrases {
		out = append(out, agents.PhraseCount{Phrase: p, Count: counts[p]})
	}
	return out
}

// headOf — головная фраза темы: самая частотная из цитат.
func headOf(queries []agents.PhraseCount) string {
	var head string
	var max int64
	for _, q := range queries {
		if q.Count > max {
			head, max = q.Phrase, q.Count
		}
	}
	return head
}

func countSelected(cands []agents.TopicCandidate) int {
	n := 0
	for _, c := range cands {
		if c.Selected {
			n++
		}
	}
	return n
}

func titlesOf(cands []agents.TopicCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.Title != "" {
			out = append(out, c.Title)
		}
	}
	return out
}
