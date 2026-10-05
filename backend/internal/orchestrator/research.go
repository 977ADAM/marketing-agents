package orchestrator

import (
	"context"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	"sort"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
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
// цитата проверена по данным (см. topicservice.Semanticist). Если спроса нет совсем или
// подтверждённых тем не хватило, добираем темы от модели с пометкой source=llm —
// без цифр, потому что цифр по ним нет.
func (o *Orchestrator) research(ctx context.Context, b campaign.Brief, p run.Progress) (campaign.Strategy, corellm.Usage, error) {
	var total corellm.Usage
	rp, hasRP := p.(run.ResearchProgress)
	stage := func(s run.ResearchStage) {
		if hasRP {
			rp.Researching(s)
		}
	}

	want := b.TopicsCount
	if want <= 0 {
		want = DefaultTopicsCount
	}
	maxCalls := o.maxWordstatCalls()
	regions := regionList(b.Region, o.opt.DefaultRegion)
	calls := 0

	// 1) Сеялки по брифу.
	stage(run.StageSeeds)
	seeds, u, err := o.semanticist.Seeds(ctx, b.Briefing(), o.seedCount())
	total = total.Add(u)
	if err != nil {
		return campaign.Strategy{}, total, fmt.Errorf("подбор тем: %w", err)
	}
	if hasRP {
		rp.ResearchSeeds(seeds)
	}
	o.traceDecision(ctx, "seeds",
		fmt.Sprintf("сеялок: %d", len(seeds)),
		map[string]any{"seeds": seeds, "requested": o.seedCount()})

	// 2) Спрос по каждой сеялке.
	stage(run.StageFetching)
	counts := map[string]int64{}
	processed := 0
	for i, seed := range seeds {
		if calls >= maxCalls {
			break
		}
		top, err := o.opt.Wordstat.Demand(ctx, topic.DemandParams{
			Phrase:     seed,
			NumPhrases: o.numPhrases(),
			Regions:    regions,
		})
		calls++
		if err != nil {
			return campaign.Strategy{}, total, fmt.Errorf("подбор тем: спрос по %q: %w", seed, err)
		}
		before := len(counts)
		collectCounts(counts, top)
		processed = i + 1
		o.traceDecision(ctx, "seed_collected",
			fmt.Sprintf("«%s»: +%d фраз, всего %d", seed, len(counts)-before, len(counts)),
			map[string]any{
				"seed": seed, "new_phrases": len(counts) - before,
				"total_phrases": len(counts), "has_data": top.HasData,
				"total_count": top.TotalCount, "cache_hit": top.CacheHit,
			})
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

	// 3) Досье для модели: фразы с частотностями как есть — без техотсева и
	// порогов, их оценивает она. Лимит нужен только чтобы промпт не распух.
	stage(run.StageSelecting)
	data := phrasesByVolume(counts, o.maxPhrases())
	var drafts []topic.TopicDraft
	if len(data) > 0 {
		drafts, u, err = o.semanticist.Select(ctx, b.Briefing(), data, want)
		total = total.Add(u)
		if err != nil {
			return campaign.Strategy{}, total, fmt.Errorf("подбор тем: %w", err)
		}
		o.traceDecision(ctx, "selection",
			fmt.Sprintf("модель собрала %d тем из %d фраз, выбрала %d (нужно %d)",
				len(drafts), len(data), chosenDraftCount(drafts), want),
			map[string]any{"phrases": len(data), "topics": len(drafts),
				"selected": chosenDraftCount(drafts), "want": want})
	}

	// 4) Кандидаты: решение модели плюс данные по её цитатам.
	cands := make([]topic.TopicCandidate, 0, len(drafts))
	for i, d := range drafts {
		queries := queriesOf(d.Queries, counts)
		c := topic.TopicCandidate{
			ID:       fmt.Sprintf("t%d", i+1),
			Title:    d.Title,
			Goal:     d.Goal,
			Task:     d.Task,
			Source:   topic.SourceWordstat,
			Selected: d.Selected,
			Reject:   d.Reject,
			Queries:  queries,
			Head:     headOf(queries),
			Intent:   d.Intent,
		}
		// Объём темы — максимум по её цитатам (не сумма: формулировки являются
		// подмножествами широкой частотности и суммирование завышает в разы).
		for _, q := range queries {
			if q.Count > c.Volume {
				c.Volume = q.Count
			}
		}
		// Тема без цитат — гипотеза модели, цифр по ней нет.
		if len(queries) == 0 {
			c.Source = topic.SourceLLM
		}
		// Сезонность — данные для показа (не фильтр): берём у выбранных тем.
		if c.Selected && c.Head != "" && calls < maxCalls {
			dyn, dynErr := o.opt.Wordstat.Dynamics(ctx, topic.DynamicsParams{
				Phrase:  c.Head,
				Period:  "monthly",
				Regions: regions,
			})
			calls++
			if dynErr == nil {
				c.Season = seasonalityOf(dyn.Points)
			}
		}
		cands = append(cands, c)
		o.traceDecision(ctx, "topic_decision",
			fmt.Sprintf("«%s»: объём %d — %s", c.Title, c.Volume, decisionNote(c)),
			map[string]any{
				"id": c.ID, "title": c.Title, "head": c.Head, "volume": c.Volume,
				"source": c.Source, "selected": c.Selected, "reject": c.Reject,
				"intent": c.Intent, "seasonal": c.Season != nil && c.Season.Seasonal,
			})
	}

	// 5) Спроса не было вовсе — темы даёт модель «от себя», без цифр.
	if chosenDraftCount(drafts) == 0 && len(counts) == 0 {
		fallback, u, err := o.semanticist.Fallback(ctx, b.Briefing(), want, nil)
		total = total.Add(u)
		if err != nil {
			return campaign.Strategy{}, total, fmt.Errorf("подбор тем: %w", err)
		}
		for i, d := range fallback {
			cands = append(cands, topic.TopicCandidate{
				ID: fmt.Sprintf("f%d", i+1), Title: d.Title, Goal: d.Goal, Task: d.Task,
				Source: topic.SourceLLM, Selected: true, Intent: d.Intent,
			})
		}
		o.traceDecision(ctx, "fallback",
			fmt.Sprintf("спроса нет: тем от модели %d", len(fallback)),
			map[string]any{"added": len(fallback), "want": want, "no_demand": true})
	}

	if chosenCount(cands) == 0 {
		return campaign.Strategy{}, total, fmt.Errorf(
			"подбор тем: модель не выбрала ни одной темы (фраз в данных: %d)", len(counts))
	}

	return campaign.Strategy{
		Topics:          chosenTopics(cands),
		TopicCandidates: cands,
		WordstatCalls:   calls,
	}, total, nil
}

// chosenCount — сколько тем выбрала модель.
func chosenCount(cands []topic.TopicCandidate) int {
	n := 0
	for _, c := range cands {
		if c.Selected {
			n++
		}
	}
	return n
}

// chosenTopics превращает выбранные темы в темы пайплайна, сохраняя порядок
// решения модели. Ритм запросов становится тезисами статьи.
func chosenTopics(cands []topic.TopicCandidate) []campaign.Topic {
	out := make([]campaign.Topic, 0, len(cands))
	for _, c := range cands {
		if !c.Selected {
			continue
		}
		points := make([]string, 0, len(c.Queries))
		for _, q := range c.Queries {
			points = append(points, q.Phrase)
		}
		out = append(out, campaign.Topic{Title: c.Title, Angle: c.Goal, Points: points})
	}
	return out
}

// seasonalityOf считает сезонную поправку по ряду dynamics: пик, дно, размах.
// Это данные для показа; на отбор тем не влияют — темы выбирает модель.
func seasonalityOf(points []topic.DynamicsPoint) *topic.Seasonality {
	if len(points) == 0 {
		return nil
	}
	peak, trough := points[0], points[0]
	for _, p := range points {
		if p.Count > peak.Count {
			peak = p
		}
		if p.Count < trough.Count {
			trough = p
		}
	}
	s := &topic.Seasonality{Peak: peak.Count, PeakMonth: monthOf(peak.Date), Trough: trough.Count}
	if trough.Count > 0 {
		s.Ratio = float64(peak.Count) / float64(trough.Count)
	}
	// Маркер для интерфейса: размах втрое и больше — тема сезонная.
	s.Seasonal = s.Ratio >= seasonalRatio
	return s
}

// seasonalRatio — с какого размаха тема помечается сезонной в интерфейсе.
// Только отображение: отбор делает модель.
const seasonalRatio = 3

// monthOf приводит дату точки к «YYYY-MM» (дата приходит в RFC3339).
func monthOf(date string) string {
	if t, err := time.Parse(time.RFC3339, date); err == nil {
		return t.Format("2006-01")
	}
	return date
}

func (o *Orchestrator) seedCount() int {
	if o.opt.SeedCount > 0 {
		return o.opt.SeedCount
	}
	return topicservice.DefaultSeedCount
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

// decisionNote описывает решение по теме для ленты трассы.
func decisionNote(c topic.TopicCandidate) string {
	switch {
	case c.Reject != "":
		return c.Reject
	case c.Selected:
		return "выбрана моделью в генерацию"
	default:
		return "модель не выбрала тему"
	}
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
func collectCounts(counts map[string]int64, top topic.Demand) {
	for _, q := range append(append([]topic.PhraseCount{}, top.Requests...), top.Associations...) {
		if q.Count > counts[q.Phrase] {
			counts[q.Phrase] = q.Count
		}
	}
}

// phrasesByVolume отдаёт фразы с частотностями по убыванию — это сырые данные
// для модели. Технический мусор и пороги здесь не фильтруются: что считать
// темой, решает модель; лимит нужен только чтобы промпт не распух.
func phrasesByVolume(counts map[string]int64, limit int) []topic.PhraseCount {
	out := make([]topic.PhraseCount, 0, len(counts))
	for phrase, count := range counts {
		out = append(out, topic.PhraseCount{Phrase: phrase, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Phrase < out[j].Phrase
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// chosenDraftCount — сколько тем модель отметила выбранными.
func chosenDraftCount(drafts []topic.TopicDraft) int {
	n := 0
	for _, d := range drafts {
		if d.Selected {
			n++
		}
	}
	return n
}

// queriesOf превращает цитаты темы в пары «фраза → частотность».
func queriesOf(phrases []string, counts map[string]int64) []topic.PhraseCount {
	out := make([]topic.PhraseCount, 0, len(phrases))
	for _, p := range phrases {
		out = append(out, topic.PhraseCount{Phrase: p, Count: counts[p]})
	}
	return out
}

// headOf — головная фраза темы: самая частотная из цитат.
func headOf(queries []topic.PhraseCount) string {
	var head string
	var max int64
	for _, q := range queries {
		if q.Count > max {
			head, max = q.Phrase, q.Count
		}
	}
	return head
}

func countSelected(cands []topic.TopicCandidate) int {
	n := 0
	for _, c := range cands {
		if c.Selected {
			n++
		}
	}
	return n
}

func titlesOf(cands []topic.TopicCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.Title != "" {
			out = append(out, c.Title)
		}
	}
	return out
}
