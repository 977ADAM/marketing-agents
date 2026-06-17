// Package orchestrator связывает агентов в пайплайн генерации кампании.
package orchestrator

import (
	"context"
	"fmt"
	"sync"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	CriticMaxIter       int
	ScoreThreshold      int
	CostPer1KPrompt     float64
	CostPer1KCompletion float64
}

// Result — итог прогона: стратегия, статьи с ревью, суммарная стоимость.
type Result struct {
	Strategy     agents.Strategy
	Deliverables []agents.Deliverable
	CostUSD      float64
}

type Orchestrator struct {
	strategist *agents.Strategist
	copywriter *agents.Copywriter
	critic     *agents.Critic
	opt        Options
}

func New(c llm.Client, opt Options) *Orchestrator {
	return &Orchestrator{
		strategist: agents.NewStrategist(c),
		copywriter: agents.NewCopywriter(c),
		critic:     agents.NewCritic(c),
		opt:        opt,
	}
}

func (o *Orchestrator) Run(ctx context.Context, b agents.Brief) (Result, error) {
	var mu sync.Mutex
	total := llm.Usage{}
	addUsage := func(u llm.Usage) {
		mu.Lock()
		total = total.Add(u)
		mu.Unlock()
	}

	strat, u, err := o.strategist.Run(ctx, b)
	if err != nil {
		return Result{}, err
	}
	addUsage(u)

	deliverables := make([]agents.Deliverable, len(strat.Topics))
	g, gctx := errgroup.WithContext(ctx)
	for i, topic := range strat.Topics {
		i, topic := i, topic
		g.Go(func() error {
			d, u, err := o.produce(gctx, b, strat, topic)
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
func (o *Orchestrator) produce(ctx context.Context, b agents.Brief, s agents.Strategy, t agents.Topic) (agents.Deliverable, llm.Usage, error) {
	total := llm.Usage{}
	art, u, err := o.copywriter.Run(ctx, b, s, t)
	total = total.Add(u)
	if err != nil {
		return agents.Deliverable{}, total, err
	}

	best := agents.Deliverable{Article: art}
	bestSet := false
	for iter := 0; iter < o.opt.CriticMaxIter; iter++ {
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
			return agents.Deliverable{Article: art, Review: rev}, total, nil
		}
		if iter == o.opt.CriticMaxIter-1 {
			break // больше не доработать — выходим с лучшим
		}
		art, u, err = o.copywriter.Revise(ctx, art, rev)
		total = total.Add(u)
		if err != nil {
			return agents.Deliverable{}, total, err
		}
	}
	return best, total, nil
}

func (o *Orchestrator) cost(u llm.Usage) float64 {
	return float64(u.PromptTokens)/1000*o.opt.CostPer1KPrompt +
		float64(u.CompletionTokens)/1000*o.opt.CostPer1KCompletion
}
