package wordstat

import (
	"context"

	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
)

// Реализация порта topic.Source: адаптер говорит на языке MCP (свои wire-типы),
// а домену отдаёт его типы. Маппинг живёт только здесь, поэтому camelCase-поля
// ответов MCP не протекают в домен.

// Demand — спрос по фразе (порт topic.Source).
func (c *Client) Demand(ctx context.Context, p topic.DemandParams) (topic.Demand, error) {
	top, err := c.topRequests(ctx, TopParams{
		Phrase:     p.Phrase,
		NumPhrases: p.NumPhrases,
		Regions:    p.Regions,
		Devices:    p.Devices,
	})
	if err != nil {
		return topic.Demand{}, err
	}
	return topic.Demand{
		Phrase:       top.Phrase,
		TotalCount:   top.TotalCount,
		Requests:     phraseCounts(top.Requests),
		Associations: phraseCounts(top.Associations),
		HasData:      top.HasData,
		CacheHit:     top.CacheHit,
	}, nil
}

// Dynamics — временной ряд по фразе (порт topic.Source).
func (c *Client) Dynamics(ctx context.Context, p topic.DynamicsParams) (topic.Dynamics, error) {
	dyn, err := c.fetchDynamics(ctx, DynamicsParams{
		Phrase:   p.Phrase,
		Period:   p.Period,
		FromDate: p.FromDate,
		ToDate:   p.ToDate,
		Regions:  p.Regions,
		Devices:  p.Devices,
	})
	if err != nil {
		return topic.Dynamics{}, err
	}
	points := make([]topic.DynamicsPoint, 0, len(dyn.Points))
	for _, pt := range dyn.Points {
		points = append(points, topic.DynamicsPoint{Date: pt.Date, Count: pt.Count, Share: pt.Share})
	}
	return topic.Dynamics{Phrase: dyn.Phrase, Period: dyn.Period, Points: points}, nil
}

// phraseCounts переводит wire-фразы в доменные.
func phraseCounts(in []PhraseCount) []topic.PhraseCount {
	if len(in) == 0 {
		return nil
	}
	out := make([]topic.PhraseCount, 0, len(in))
	for _, p := range in {
		out = append(out, topic.PhraseCount{Phrase: p.Phrase, Count: p.Count})
	}
	return out
}
