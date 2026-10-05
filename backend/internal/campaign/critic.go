package campaign

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/corellm"

	"github.com/977ADAM/marketing-agents/internal/score"
)

const RoleCritic = "critic"

const criticSystem = `Ты — строгий редактор. Оцени статью от 0 до 100 и дай замечания. Ответ строго в JSON:
{"score": <0-100>, "issues": ["..."], "verdict": "accept"|"revise"}.
verdict="accept" если статья готова к публикации, иначе "revise".
Замечания (issues) пиши на русском. Снижай оценку, если статья не на русском языке.`

type Critic struct{ llm corellm.Client }

func NewCritic(c corellm.Client) *Critic { return &Critic{llm: c} }

func (cr *Critic) Run(ctx context.Context, b Brief, a Article) (Review, corellm.Usage, error) {
	user := fmt.Sprintf("Аудитория: %s; тон: %s.\nЗаголовок: %s\nТекст: %s\nCTA: %s",
		b.Audience, b.Tone, a.Title, a.Body, a.CTA)
	var out Review
	usage, err := cr.llm.Complete(ctx, RoleCritic, criticSystem, user, &out)
	if err != nil {
		return Review{}, usage, fmt.Errorf("critic: %w", err)
	}
	if out.Score < 0 {
		out.Score = 0
	}
	if out.Score > 100 {
		out.Score = 100
	}
	out.Severity = score.Severity(out.Score)
	return out, usage, nil
}
