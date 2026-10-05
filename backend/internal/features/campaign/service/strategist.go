package campaignservice

import (
	"context"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
)

const RoleStrategist = "strategist"

const strategistSystem = `Ты — маркетинговый стратег. По брифу определи позиционирование и
разбей кампанию на несколько тем для статей (нативная реклама). Ответ строго в JSON:
{"positioning": "...", "topics": [{"title": "...", "angle": "...", "points": ["..."]}]}.
Соблюдай число тем из брифа; если число не задано, предложи от 2 до 5 тем. Каждая с понятным углом подачи.
Все тексты (позиционирование, заголовки тем, тезисы) — на русском языке.`

type Strategist struct{ llm corellm.Client }

func NewStrategist(c corellm.Client) *Strategist { return &Strategist{llm: c} }

func (s *Strategist) Run(ctx context.Context, b campaign.Brief) (campaign.Strategy, corellm.Usage, error) {
	user := fmt.Sprintf("Продукт: %s\nЦель: %s\nАудитория: %s\nТон: %s",
		b.Product, b.Goal, b.Audience, b.Tone)
	if b.TopicsCount > 0 {
		user += fmt.Sprintf("\nЧисло тем: ровно %d.", b.TopicsCount)
	}
	var out campaign.Strategy
	usage, err := s.llm.Complete(ctx, RoleStrategist, strategistSystem, user, &out)
	if err != nil {
		return campaign.Strategy{}, usage, fmt.Errorf("strategist: %w", err)
	}
	if len(out.Topics) == 0 {
		return campaign.Strategy{}, usage, fmt.Errorf("strategist: no topics returned")
	}
	return out, usage, nil
}
