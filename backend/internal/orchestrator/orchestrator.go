// Package orchestrator связывает агентов в пайплайн генерации кампании.
package orchestrator

import (
	"context"
	"fmt"
	"sync"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	CriticMaxIter       int
	ScoreThreshold      int
	CostPer1KPrompt     float64
	CostPer1KCompletion float64
	MaxTopics           int // 0 = без ограничения; иначе кап на число тем (контроль стоимости/конкуррентности)

	// Wordstat — источник спроса на темы. nil (или nil Semanticist) означает,
	// что подбор тем выключен: темы даёт стратег, как до появления Wordstat.
	Wordstat wordstat.Source
	// Semanticist — агент подбора тем: сеялки, кластеризация, fallback.
	Semanticist *agents.Semanticist
	// Select — правила отбора тем (порог объёма, множитель сезонности).
	Select SelectOptions
	// TopicsMultiplier — во сколько раз больше тем предлагать, чем нужно статей.
	TopicsMultiplier int
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
}

// Result — итог прогона: стратегия, статьи с ревью, суммарная стоимость.
type Result struct {
	Strategy     agents.Strategy
	Deliverables []agents.Deliverable
	CostUSD      float64
}

type Orchestrator struct {
	llm         llm.Client
	strategist  *agents.Strategist
	copywriter  *agents.Copywriter
	critic      *agents.Critic
	semanticist *agents.Semanticist
	opt         Options
}

func New(c llm.Client, opt Options) *Orchestrator {
	semanticist := opt.Semanticist
	if semanticist == nil {
		semanticist = agents.NewSemanticist(c)
	}
	return &Orchestrator{
		llm:         c,
		strategist:  agents.NewStrategist(c),
		copywriter:  agents.NewCopywriter(c),
		critic:      agents.NewCritic(c),
		semanticist: semanticist,
		opt:         opt,
	}
}

// canResearch сообщает, настроен ли подбор тем по спросу.
func (o *Orchestrator) canResearch() bool {
	return o.opt.Wordstat != nil && o.semanticist != nil
}

func (o *Orchestrator) Run(ctx context.Context, b agents.Brief, p Progress) (Result, error) {
	if p == nil {
		p = NopProgress{}
	}
	var mu sync.Mutex
	total := llm.Usage{}
	addUsage := func(u llm.Usage) {
		mu.Lock()
		total = total.Add(u)
		mu.Unlock()
	}

	// Темы: либо подбор на поисковом спросе, либо стратег как раньше.
	// Позиционирование в обоих случаях даёт стратег.
	var strat agents.Strategy
	if o.canResearch() {
		researched, u, err := o.research(ctx, b, p)
		if err != nil {
			return Result{}, err
		}
		addUsage(u)
		strat = researched

		p.Strategizing()
		st, u, err := o.strategist.Run(ctx, b)
		if err != nil {
			return Result{}, err
		}
		addUsage(u)
		strat.Positioning = st.Positioning
	} else {
		p.Strategizing()
		st, u, err := o.strategist.Run(ctx, b)
		if err != nil {
			return Result{}, err
		}
		addUsage(u)
		strat = st
	}

	// Кап на число тем: подбор мог вернуть больше, чем хотим обрабатывать.
	if o.opt.MaxTopics > 0 && len(strat.Topics) > o.opt.MaxTopics {
		strat.Topics = strat.Topics[:o.opt.MaxTopics]
	}

	titles := make([]string, len(strat.Topics))
	for i, t := range strat.Topics {
		titles[i] = t.Title
	}
	p.TopicsPlanned(titles)

	deliverables := make([]agents.Deliverable, len(strat.Topics))
	g, gctx := errgroup.WithContext(ctx)
	for i, topic := range strat.Topics {
		i, topic := i, topic
		g.Go(func() error {
			d, u, err := o.produce(gctx, b, strat, i, topic, p)
			addUsage(u)
			if err != nil {
				return fmt.Errorf("topic %q: %w", topic.Title, err)
			}
			deliverables[i] = d
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Result{}, err
	}

	return Result{
		Strategy:     strat,
		Deliverables: deliverables,
		CostUSD:      o.cost(total),
	}, nil
}

// produce пишет статью и гоняет цикл критика; usage аккумулируется по всем вызовам.
func (o *Orchestrator) produce(ctx context.Context, b agents.Brief, s agents.Strategy, i int, t agents.Topic, p Progress) (agents.Deliverable, llm.Usage, error) {
	total := llm.Usage{}
	p.TopicWriting(i)
	art, u, err := o.copywriter.Run(ctx, b, s, t)
	total = total.Add(u)
	if err != nil {
		return agents.Deliverable{}, total, err
	}

	best := agents.Deliverable{Article: art}
	bestSet := false
	for iter := 0; iter < o.opt.CriticMaxIter; iter++ {
		p.TopicReviewing(i, iter+1)
		rev, u, err := o.critic.Run(ctx, b, art)
		total = total.Add(u)
		if err != nil {
			return agents.Deliverable{}, total, err
		}
		if !bestSet || rev.Score > best.Review.Score {
			best = agents.Deliverable{Article: art, Review: rev}
			bestSet = true
		}
		if rev.Verdict == "accept" || rev.Score >= o.opt.ScoreThreshold {
			p.TopicDone(i, rev.Score)
			return agents.Deliverable{Article: art, Review: rev}, total, nil
		}
		if iter == o.opt.CriticMaxIter-1 {
			break // больше не доработать — выходим с лучшим
		}
		p.TopicRevising(i, iter+1)
		art, u, err = o.copywriter.Revise(ctx, art, rev)
		total = total.Add(u)
		if err != nil {
			return agents.Deliverable{}, total, err
		}
	}
	p.TopicDone(i, best.Review.Score)
	return best, total, nil
}

func (o *Orchestrator) cost(u llm.Usage) float64 {
	return float64(u.PromptTokens)/1000*o.opt.CostPer1KPrompt +
		float64(u.CompletionTokens)/1000*o.opt.CostPer1KCompletion
}
