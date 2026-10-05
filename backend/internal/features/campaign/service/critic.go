package campaignservice

import (
	"context"
	"encoding/json"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"

	score "github.com/977ADAM/marketing-agents/internal/core/score"
)

const RoleCritic = "critic"

const criticSystem = `Ты — строгий редактор. Оцени статью от 0 до 100 и дай замечания. Ответ строго в JSON:
{"score": <0-100>, "issues": ["..."], "verdict": "accept"|"revise"}.
verdict="accept" если статья готова к публикации, иначе "revise".
Замечания (issues) пиши на русском. Снижай оценку, если статья не на русском языке.`

type Critic struct{ llm corellm.Client }

func NewCritic(c corellm.Client) *Critic { return &Critic{llm: c} }

func (cr *Critic) Run(ctx context.Context, b campaign.Brief, s campaign.Strategy, t campaign.Topic, a campaign.Article) (campaign.Review, corellm.Usage, error) {
	input, _ := json.Marshal(struct {
		Brief    campaign.Brief
		Strategy campaign.Strategy
		Topic    campaign.Topic
		Article  campaign.Article
	}{b, s, t, a})
	user := string(input)
	var out struct {
		Score   *int     `json:"score"`
		Issues  []string `json:"issues"`
		Verdict string   `json:"verdict"`
	}
	usage, err := cr.llm.Complete(ctx, RoleCritic, criticSystem, user, &out)
	if err != nil {
		return campaign.Review{}, usage, fmt.Errorf("critic: %w", err)
	}
	if out.Score == nil || *out.Score < 0 || *out.Score > 100 {
		return campaign.Review{}, usage, fmt.Errorf("critic: missing or invalid score")
	}
	if out.Verdict != "accept" && out.Verdict != "revise" {
		return campaign.Review{}, usage, fmt.Errorf("critic: invalid verdict")
	}
	if out.Issues == nil {
		return campaign.Review{}, usage, fmt.Errorf("critic: missing issues")
	}
	return campaign.Review{Score: *out.Score, Issues: out.Issues, Verdict: out.Verdict, Severity: score.Severity(*out.Score)}, usage, nil
}
