package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/llm"
)

const RoleCopywriter = "copywriter"

const copywriterSystem = `Ты — копирайтер нативных статей. Напиши статью по теме строго в JSON:
{"topic": "...", "title": "...", "body": "...", "cta": "..."}.
body — связный текст статьи; cta — призыв к действию. Учитывай тон и аудиторию из брифа.
Пиши на русском языке (title, body, cta — всё по-русски).`

type Copywriter struct{ llm llm.Client }

func NewCopywriter(c llm.Client) *Copywriter { return &Copywriter{llm: c} }

func (cw *Copywriter) Run(ctx context.Context, b campaign.Brief, s campaign.Strategy, t campaign.Topic) (campaign.Article, llm.Usage, error) {
	user := fmt.Sprintf(
		"Бриф — продукт: %s; цель: %s; аудитория: %s; тон: %s.\nПозиционирование: %s.\nТема: %s\nУгол: %s\nТезисы: %v",
		b.Product, b.Goal, b.Audience, b.Tone, s.Positioning, t.Title, t.Angle, t.Points)
	return cw.complete(ctx, copywriterSystem, user)
}

func (cw *Copywriter) Revise(ctx context.Context, prev campaign.Article, r campaign.Review) (campaign.Article, llm.Usage, error) {
	prevJSON, _ := json.Marshal(prev)
	user := fmt.Sprintf(
		"Доработай статью с учётом замечаний критика.\nТекущая версия: %s\nОценка: %d\nЗамечания: %v\nВерни улучшенную статью в том же JSON-формате.",
		string(prevJSON), r.Score, r.Issues)
	return cw.complete(ctx, copywriterSystem, user)
}

func (cw *Copywriter) complete(ctx context.Context, system, user string) (campaign.Article, llm.Usage, error) {
	var out campaign.Article
	usage, err := cw.llm.Complete(ctx, RoleCopywriter, system, user, &out)
	if err != nil {
		return campaign.Article{}, usage, fmt.Errorf("copywriter: %w", err)
	}
	if out.Title == "" || out.Body == "" {
		return campaign.Article{}, usage, fmt.Errorf("copywriter: incomplete article")
	}
	return out, usage, nil
}
