package orchestrator

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
	"github.com/977ADAM/marketing-agents/internal/score"
)

// Review прогоняет готовые тексты через двух агентов (соответствие брифу и
// корректность текста) параллельно по текстам и возвращает отчёты.
func (o *Orchestrator) Review(ctx context.Context, req review.Request, p run.Progress) (review.Result, error) {
	if p == nil {
		p = run.NopProgress{}
	}
	var mu sync.Mutex
	total := llm.Usage{}
	addUsage := func(u llm.Usage) {
		mu.Lock()
		total = total.Add(u)
		mu.Unlock()
	}

	titles := make([]string, len(req.Texts))
	for i, t := range req.Texts {
		titles[i] = titleOf(t, i)
	}
	p.TopicsPlanned(titles)

	compliance := agents.NewComplianceChecker(o.llm)
	quality := agents.NewQualityChecker(o.llm)

	reports := make([]review.TextReport, len(req.Texts))
	g, gctx := errgroup.WithContext(ctx)
	for i, t := range req.Texts {
		i, t := i, t
		g.Go(func() error {
			report, u, err := o.reviewOne(gctx, compliance, quality, req.BriefText, i, t, p)
			addUsage(u)
			if err != nil {
				return fmt.Errorf("text %q: %w", titleOf(t, i), err)
			}
			reports[i] = report
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return review.Result{}, err
	}

	passed := 0
	for _, r := range reports {
		if r.Verdict == review.VerdictPass {
			passed++
		}
	}
	return review.Result{Items: reports, Passed: passed, CostUSD: o.cost(total)}, nil
}

// reviewOne — проверка одного текста двумя агентами с прогрессом.
func (o *Orchestrator) reviewOne(ctx context.Context, compliance *agents.ComplianceChecker, quality *agents.QualityChecker,
	briefText string, i int, t review.TextToReview, p run.Progress) (review.TextReport, llm.Usage, error) {
	var total llm.Usage

	p.TopicWriting(i) // первый агент: соответствие брифу
	c, u, err := compliance.Run(ctx, briefText, t)
	total = total.Add(u)
	if err != nil {
		return review.TextReport{}, total, err
	}

	p.TopicReviewing(i, 1) // второй агент: корректность текста
	q, u, err := quality.Run(ctx, t)
	total = total.Add(u)
	if err != nil {
		return review.TextReport{}, total, err
	}

	overall := c.Score
	if q.Score < overall {
		overall = q.Score
	}
	p.TopicDone(i, overall)
	return review.TextReport{
		Title:      titleOf(t, i),
		Compliance: c,
		Quality:    q,
		Overall:    overall,
		Verdict:    review.Verdict(c.Score, q.Score),
		Severity:   score.Severity(overall),
	}, total, nil
}

// titleOf — заголовок текста для прогресса; пустой заменяем на «Текст N».
func titleOf(t review.TextToReview, i int) string {
	if t.Title != "" {
		return t.Title
	}
	return fmt.Sprintf("Текст %d", i+1)
}
