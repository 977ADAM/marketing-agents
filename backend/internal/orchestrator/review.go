package orchestrator

import (
	"context"
	"fmt"
	"sync"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"golang.org/x/sync/errgroup"
)

// ReviewRequest — вход проверки готовых текстов: бриф + тексты.
type ReviewRequest struct {
	BriefText string                `json:"brief"`
	Texts     []agents.TextToReview `json:"texts"`
}

// ReviewResult — итог проверки: отчёты по текстам, сводка и суммарная стоимость.
// Passed — сколько текстов прошло (verdict=pass): считает бэкенд, фронт только
// показывает готовое число.
type ReviewResult struct {
	Items   []agents.TextReport `json:"items"`
	Passed  int                 `json:"passed"`
	CostUSD float64             `json:"cost_usd"`
}

// Review прогоняет готовые тексты через двух агентов (соответствие брифу и
// корректность текста) параллельно по текстам и возвращает отчёты.
func (o *Orchestrator) Review(ctx context.Context, req ReviewRequest, p Progress) (ReviewResult, error) {
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

	titles := make([]string, len(req.Texts))
	for i, t := range req.Texts {
		titles[i] = titleOf(t, i)
	}
	p.TopicsPlanned(titles)

	compliance := agents.NewComplianceChecker(o.llm)
	quality := agents.NewQualityChecker(o.llm)

	reports := make([]agents.TextReport, len(req.Texts))
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
		return ReviewResult{}, err
	}

	passed := 0
	for _, r := range reports {
		if r.Verdict == agents.VerdictPass {
			passed++
		}
	}
	return ReviewResult{Items: reports, Passed: passed, CostUSD: o.cost(total)}, nil
}

// reviewOne — проверка одного текста двумя агентами с прогрессом.
func (o *Orchestrator) reviewOne(ctx context.Context, compliance *agents.ComplianceChecker, quality *agents.QualityChecker,
	briefText string, i int, t agents.TextToReview, p Progress) (agents.TextReport, llm.Usage, error) {
	var total llm.Usage

	p.TopicWriting(i) // первый агент: соответствие брифу
	c, u, err := compliance.Run(ctx, briefText, t)
	total = total.Add(u)
	if err != nil {
		return agents.TextReport{}, total, err
	}

	p.TopicReviewing(i, 1) // второй агент: корректность текста
	q, u, err := quality.Run(ctx, t)
	total = total.Add(u)
	if err != nil {
		return agents.TextReport{}, total, err
	}

	overall := c.Score
	if q.Score < overall {
		overall = q.Score
	}
	p.TopicDone(i, overall)
	return agents.TextReport{
		Title:      titleOf(t, i),
		Compliance: c,
		Quality:    q,
		Overall:    overall,
		Verdict:    agents.Verdict(c.Score, q.Score),
		Severity:   agents.Severity(overall),
	}, total, nil
}

// titleOf — заголовок текста для прогресса; пустой заменяем на «Текст N».
func titleOf(t agents.TextToReview, i int) string {
	if t.Title != "" {
		return t.Title
	}
	return fmt.Sprintf("Текст %d", i+1)
}
