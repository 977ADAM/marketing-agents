package reviewservice

import (
	"context"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	"golang.org/x/sync/errgroup"
	"sync"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	score "github.com/977ADAM/marketing-agents/internal/core/score"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

// Review прогоняет готовые тексты через двух агентов (соответствие брифу и
// корректность текста) параллельно по текстам и возвращает отчёты.
func (o *Workflow) Review(ctx context.Context, req review.Request, p run.Progress) (res review.Result, err error) {
	if p == nil {
		p = run.NopProgress{}
	}
	checkpoints := run.Checkpoints{Store: o.opt.Checkpoints, ID: trace.RunIDFrom(ctx)}
	var mu sync.Mutex
	total := corellm.Usage{}
	addUsage := func(u corellm.Usage) {
		mu.Lock()
		total = total.Add(u)
		mu.Unlock()
	}

	defer func() {
		mu.Lock()
		usage := total
		mu.Unlock()
		res.CostUSD = o.cost(usage)
		if saveErr := checkpoints.Save(ctx, "summary", 0, run.RunSummary{CostUSD: res.CostUSD, Usage: usage}); saveErr != nil && err == nil {
			err = saveErr
		}
	}()

	titles := make([]string, len(req.Texts))
	for i, t := range req.Texts {
		titles[i] = titleOf(t, i)
	}
	p.TopicsPlanned(titles)

	compliance := NewComplianceChecker(o.llm)
	quality := NewQualityChecker(o.llm)

	reports := make([]review.TextReport, len(req.Texts))
	res.Items = reports
	g, gctx := errgroup.WithContext(ctx)
	parallel := o.opt.ParallelTexts
	if parallel <= 0 {
		parallel = 4
	}
	g.SetLimit(parallel)
	for i, t := range req.Texts {
		i, t := i, t
		g.Go(func() error {
			var saved run.Saved[review.TextReport]
			found, err := checkpoints.Load(gctx, "text", i, &saved)
			if err != nil {
				return err
			}
			if found {
				reports[i] = saved.Value
				addUsage(saved.Usage)
				p.TopicDone(i, saved.Value.Overall)
				return nil
			}
			report, u, err := o.reviewOne(gctx, compliance, quality, req.BriefText, i, t, p)
			addUsage(u)
			if err != nil {
				return fmt.Errorf("text %q: %w", titleOf(t, i), err)
			}
			if err := checkpoints.Save(gctx, "text", i, run.Saved[review.TextReport]{Value: report, Usage: u}); err != nil {
				return err
			}
			reports[i] = report
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return res, err
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
func (o *Workflow) reviewOne(ctx context.Context, compliance *ComplianceChecker, quality *QualityChecker,
	briefText string, i int, t review.TextToReview, p run.Progress) (review.TextReport, corellm.Usage, error) {
	var total corellm.Usage

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
