// Package orchestrator связывает агентов в пайплайн генерации кампании.
package campaignservice

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	"golang.org/x/sync/errgroup"
	"sync"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

type Options struct {
	Prices              *corellm.Prices
	CriticMaxIter       int
	ScoreThreshold      int
	CostPer1KPrompt     float64
	CostPer1KCompletion float64
	MaxTopics           int // 0 = без ограничения; иначе кап на число тем (контроль стоимости/конкуррентности)

	// Wordstat — источник спроса на темы. nil (или nil Semanticist) означает,
	// что подбор тем выключен: темы даёт стратег, как до появления Wordstat.
	Wordstat topicservice.Source
	// Semanticist — агент подбора тем: сеялки, кластеризация, fallback.
	Semanticist *topicservice.Semanticist
	// SeedCount — сколько сеялок просить у модели (0 — дефолт).
	SeedCount int
	// NumPhrases — сколько фраз запрашивать у Wordstat на сеялку (0 — дефолт 50).
	NumPhrases int
	// MaxWordstatCalls — лимит обращений к Wordstat на прогон (0 — дефолт 60).
	MaxWordstatCalls int
	// MaxPhrases — сколько фраз отдавать модели на кластеризацию (0 — дефолт 40).
	MaxPhrases int
	// DefaultRegion — geo ID региона, если бриф его не задал.
	DefaultRegion string
	// Recorder — журнал событий прогона (nil — трасса не пишется).
	Recorder      trace.Recorder
	ParallelTexts int
	Checkpoints   run.CheckpointStore
}

// Result — итог прогона: стратегия, статьи с ревью, суммарная стоимость и расход.
type Result struct {
	Strategy     campaign.Strategy
	Deliverables []campaign.Deliverable
	CostUSD      float64
	CostKnown    bool
	// Usage — суммарные токены прогона (трасса показывает их по ролям в событиях,
	// здесь — общий итог).
	Usage corellm.Usage
}

type Workflow struct {
	llm        corellm.Client
	strategist *Strategist
	copywriter *Copywriter
	critic     *Critic
	researcher *topicservice.Workflow
	trace      trace.Recorder
	opt        Options
}

func NewWorkflow(c corellm.Client, opt Options) *Workflow {
	semanticist := opt.Semanticist
	if semanticist == nil {
		semanticist = topicservice.NewSemanticist(c)
	}
	return &Workflow{
		llm:        c,
		strategist: NewStrategist(c),
		copywriter: NewCopywriter(c),
		critic:     NewCritic(c),
		researcher: topicservice.NewWorkflow(c, topicservice.Options{Wordstat: opt.Wordstat, Semanticist: semanticist, SeedCount: opt.SeedCount, NumPhrases: opt.NumPhrases, MaxWordstatCalls: opt.MaxWordstatCalls, MaxPhrases: opt.MaxPhrases, DefaultRegion: opt.DefaultRegion, Recorder: opt.Recorder}),
		trace:      trace.OrNop(opt.Recorder),
		opt:        opt,
	}
}

// canResearch сообщает, настроен ли подбор тем по спросу.
func (o *Workflow) canResearch() bool {
	return o.opt.Wordstat != nil && o.researcher != nil
}

func (o *Workflow) Run(ctx context.Context, b campaign.Brief, p run.Progress) (res Result, err error) {
	if p == nil {
		p = run.NopProgress{}
	}
	if b.TopicsCount < 0 || (o.opt.MaxTopics > 0 && b.TopicsCount > o.opt.MaxTopics) {
		return Result{}, limits.Invalid("requested topics exceed configured cap")
	}
	checkpoints := run.Checkpoints{Store: o.opt.Checkpoints, ID: trace.RunIDFrom(ctx)}
	var mu sync.Mutex
	total := corellm.Usage{}
	addUsage := func(u corellm.Usage) {
		mu.Lock()
		total = total.Add(u)
		mu.Unlock()
	}
	// Итог прогона пишем в трассу в любом случае: и при успехе, и при ошибке —
	// иначе провалившийся прогон остаётся без объяснения, ради чего трасса и нужна.
	defer func() {
		mu.Lock()
		res.Usage = total
		mu.Unlock()
		if store, ok := o.opt.Checkpoints.(corellm.UsageStore); ok && checkpoints.ID != "" {
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			entries, loadErr := store.UsageEntries(final, checkpoints.ID)
			cancel()
			if loadErr != nil && err == nil {
				err = loadErr
			}
			if len(entries) > 0 {
				res.Usage = corellm.FromEntries(entries)
			}
		}
		res.CostUSD, res.CostKnown = corellm.EstimateUsage(res.Usage, o.opt.Prices, corellm.Rates{Prompt: o.opt.CostPer1KPrompt, Completion: o.opt.CostPer1KCompletion})
		if saveErr := checkpoints.Save(ctx, "summary", 0, run.RunSummary{CostUSD: res.CostUSD, CostKnown: res.CostKnown, Usage: res.Usage}); saveErr != nil && err == nil {
			err = saveErr
		}
		o.traceResult(ctx, res, err)
	}()

	// Темы: либо подбор на поисковом спросе, либо стратег как раньше.
	// Позиционирование в обоих случаях даёт стратег.
	var strat campaign.Strategy
	var savedStrategy run.Saved[campaign.Strategy]
	strategyFound, loadErr := checkpoints.Load(ctx, "strategy", 0, &savedStrategy)
	if loadErr != nil {
		return res, loadErr
	}
	if strategyFound {
		strat = savedStrategy.Value
		addUsage(savedStrategy.Usage)
	} else {
		if o.canResearch() {
			var researched topic.ResearchResult
			var u corellm.Usage
			var research run.Saved[topic.ResearchResult]
			researchFound, err := checkpoints.Load(ctx, "research", 0, &research)
			if err != nil {
				return res, err
			}
			if researchFound {
				researched = research.Value
				u = research.Usage
			} else {
				o.tracePhase(ctx, "researching", "подбор тем по поисковому спросу")
				researched, u, err = o.researcher.Run(ctx, topic.ResearchRequest{Briefing: b.Briefing(), Region: b.Region, TopicsCount: b.TopicsCount}, p)
				if err == nil {
					err = checkpoints.Save(ctx, "research", 0, run.Saved[topic.ResearchResult]{Value: researched, Usage: u})
				}
			}
			addUsage(u)
			if err != nil {
				return res, err
			}
			strat.TopicCandidates = researched.TopicCandidates
			strat.WordstatCalls = researched.WordstatCalls
			for _, t := range researched.Topics {
				strat.Topics = append(strat.Topics, campaign.Topic{Title: t.Title, Angle: t.Angle, Points: t.Points})
			}

			p.Strategizing()
			o.tracePhase(ctx, "strategizing", "позиционирование по отобранным темам")
			st, u, err := o.strategist.Run(ctx, b)
			addUsage(u)
			if err != nil {
				return res, err
			}
			strat.Positioning = st.Positioning
		} else {
			p.Strategizing()
			o.tracePhase(ctx, "strategizing", "позиционирование и темы от стратега")
			st, u, err := o.strategist.Run(ctx, b)
			addUsage(u)
			if err != nil {
				return res, err
			}
			strat = st
		}

		cap := b.TopicsCount
		if cap == 0 {
			cap = o.opt.MaxTopics
		}
		if cap > 0 && len(strat.Topics) > cap {
			strat.Topics = strat.Topics[:cap]
		}
		if b.TopicsCount > 0 && len(strat.Topics) < b.TopicsCount {
			strat.Warnings = append(strat.Warnings, fmt.Sprintf("Запрошено %d тем, подобрано %d", b.TopicsCount, len(strat.Topics)))
		}

		if err := checkpoints.Save(ctx, "strategy", 0, run.Saved[campaign.Strategy]{Value: strat, Usage: total}); err != nil {
			return res, err
		}
	}
	res.Strategy = strat

	titles := make([]string, len(strat.Topics))
	for i, t := range strat.Topics {
		titles[i] = t.Title
	}
	p.TopicsPlanned(titles)

	deliverables := make([]campaign.Deliverable, len(strat.Topics))
	res.Deliverables = deliverables
	g, gctx := errgroup.WithContext(ctx)
	parallel := o.opt.ParallelTexts
	if parallel <= 0 {
		parallel = 4
	}
	g.SetLimit(parallel)
	for i, topic := range strat.Topics {
		i, topic := i, topic
		g.Go(func() error {
			var saved run.Saved[campaign.Deliverable]
			found, err := checkpoints.Load(gctx, "article", i, &saved)
			if err != nil {
				return err
			}
			if found {
				deliverables[i] = saved.Value
				addUsage(saved.Usage)
				score := 0
				if saved.Value.Review != nil {
					score = saved.Value.Review.Score
				}
				p.TopicDone(i, score)
				return nil
			}
			d, u, err := o.produce(gctx, b, strat, i, topic, p)
			addUsage(u)
			if err != nil {
				return fmt.Errorf("topic %q: %w", topic.Title, err)
			}
			if err := checkpoints.Save(gctx, "article", i, run.Saved[campaign.Deliverable]{Value: d, Usage: u}); err != nil {
				return err
			}
			deliverables[i] = d
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return res, err
	}

	return Result{
		Strategy:     strat,
		Deliverables: deliverables,
		CostUSD:      o.cost(total),
	}, nil
}

// produce пишет статью и гоняет цикл критика; usage аккумулируется по всем вызовам.
func (o *Workflow) produce(ctx context.Context, b campaign.Brief, s campaign.Strategy, i int, t campaign.Topic, p run.Progress) (campaign.Deliverable, corellm.Usage, error) {
	total := corellm.Usage{}
	o.tracePhase(ctx, "producing", fmt.Sprintf("статья %d из %d: «%s»", i+1, len(s.Topics), t.Title))
	p.TopicWriting(i)
	art, u, err := o.copywriter.Run(ctx, b, s, t)
	total = total.Add(u)
	if err != nil {
		return campaign.Deliverable{}, total, err
	}

	best := campaign.Deliverable{Article: art}
	bestSet := false
	for iter := 0; iter < o.opt.CriticMaxIter; iter++ {
		p.TopicReviewing(i, iter+1)
		rev, u, err := o.critic.Run(ctx, b, s, t, art)
		total = total.Add(u)
		if err != nil {
			return campaign.Deliverable{}, total, err
		}
		o.traceDecision(ctx, "critic", fmt.Sprintf("«%s»: итерация %d — %d/100, %s, замечаний %d",
			t.Title, iter+1, rev.Score, verdictNote(rev.Verdict), len(rev.Issues)),
			map[string]any{
				"topic": t.Title, "iteration": iter + 1, "score": rev.Score,
				"verdict": rev.Verdict, "issues": rev.Issues,
			})

		if !bestSet || rev.Score > best.Review.Score {
			best = campaign.Deliverable{Article: art, Review: &rev}
			bestSet = true
		}
		if rev.Verdict == "accept" || rev.Score >= o.opt.ScoreThreshold {
			p.TopicDone(i, rev.Score)
			return campaign.Deliverable{Article: art, Review: &rev}, total, nil
		}
		if iter == o.opt.CriticMaxIter-1 {
			break // больше не доработать — выходим с лучшим
		}
		p.TopicRevising(i, iter+1)
		art, u, err = o.copywriter.Revise(ctx, b, s, t, art, rev)
		total = total.Add(u)
		if err != nil {
			return campaign.Deliverable{}, total, err
		}
	}
	finalScore := 0
	if best.Review != nil {
		finalScore = best.Review.Score
	}
	p.TopicDone(i, finalScore)
	return best, total, nil
}

// verdictNote переводит вердикт критика в человеческое слово для ленты трассы.
func verdictNote(verdict string) string {
	if verdict == "accept" {
		return "принято"
	}
	return "на доработку"
}

func (o *Workflow) cost(u corellm.Usage) float64 {
	return float64(u.PromptTokens)/1000*o.opt.CostPer1KPrompt +
		float64(u.CompletionTokens)/1000*o.opt.CostPer1KCompletion
}
