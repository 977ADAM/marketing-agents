package reviewservice

import (
	"context"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"

	score "github.com/977ADAM/marketing-agents/internal/core/score"
)

// Роли агентов проверки готовых текстов. По умолчанию обе идут на MODEL_DEFAULT
// (рассуждения важнее скорости); при желании можно разнести SetRoleModel.
const (
	RoleCompliance = "compliance" // соответствие брифу
	RoleQuality    = "quality"    // корректность самого текста
)

const complianceSystem = `Ты — редактор, проверяющий соответствие готового текста брифу клиента.
Сверь текст с брифом по всем пунктам: продукт и его УТП, целевая аудитория, ключевые сообщения,
обязательные элементы (отработка возражений, слоган, сервисные преимущества), запреты, тон и стиль.
Ответ строго в JSON: {"score": <0-100>, "issues": ["..."]}.
score — насколько текст соответствует брифу (100 = полное соответствие).
issues — конкретные несоответствия брифу, каждое на русском, с указанием пункта брифа.
Если несоответствий нет, issues = [].`

const qualitySystem = `Ты — строгий редактор, проверяющий корректность самого текста.
Проверь: фактические ошибки и неподтверждённые утверждения, орфографию, грамматику, пунктуацию,
согласование, стиль и читаемость, логические противоречия, дубли и тавтологию, корректность
специальных терминов, цифр, названий моделей и типоразмеров.
Ответ строго в JSON: {"score": <0-100>, "issues": ["..."]}.
score — качество текста (100 = без замечаний).
issues — конкретные замечания с примерами из текста, каждое на русском.
Если замечаний нет, issues = [].`

// ComplianceChecker — агент «соответствие брифу».
type ComplianceChecker struct{ llm corellm.Client }

func NewComplianceChecker(c corellm.Client) *ComplianceChecker { return &ComplianceChecker{llm: c} }

func (ch *ComplianceChecker) Run(ctx context.Context, briefText string, t review.TextToReview) (review.CheckScore, corellm.Usage, error) {
	user := fmt.Sprintf("БРИФ:\n%s\n\nТЕКСТ ДЛЯ ПРОВЕРКИ:\nЗаголовок: %s\n\n%s",
		briefText, t.Title, t.Body)
	var out review.CheckScore
	usage, err := ch.llm.Complete(ctx, RoleCompliance, complianceSystem, user, &out)
	if err != nil {
		return review.CheckScore{}, usage, fmt.Errorf("compliance: %w", err)
	}
	out.Score = clampScore(out.Score)
	out.Severity = score.Severity(out.Score)
	return out, usage, nil
}

// QualityChecker — агент «корректность текста».
type QualityChecker struct{ llm corellm.Client }

func NewQualityChecker(c corellm.Client) *QualityChecker { return &QualityChecker{llm: c} }

func (q *QualityChecker) Run(ctx context.Context, t review.TextToReview) (review.CheckScore, corellm.Usage, error) {
	user := fmt.Sprintf("Заголовок: %s\n\n%s", t.Title, t.Body)
	var out review.CheckScore
	usage, err := q.llm.Complete(ctx, RoleQuality, qualitySystem, user, &out)
	if err != nil {
		return review.CheckScore{}, usage, fmt.Errorf("quality: %w", err)
	}
	out.Score = clampScore(out.Score)
	out.Severity = score.Severity(out.Score)
	return out, usage, nil
}

func clampScore(s int) int {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}
