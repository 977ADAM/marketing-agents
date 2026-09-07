package agents

import (
	"context"
	"fmt"

	"github.com/977ADAM/marketing-agents/internal/llm"
)

// Роли агентов проверки готовых текстов. По умолчанию обе идут на MODEL_DEFAULT
// (рассуждения важнее скорости); при желании можно разнести SetRoleModel.
const (
	RoleCompliance = "compliance" // соответствие брифу
	RoleQuality    = "quality"    // корректность самого текста
)

// TextToReview — готовая статья, которую проверяют агенты.
type TextToReview struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// CheckScore — оценка одного агента по одному измерению.
type CheckScore struct {
	Score  int      `json:"score"`
	Issues []string `json:"issues"`
}

// TextReport — итог проверки одного текста. Verdict: "pass" | "fix".
// Overall — min(compliance, quality): слабое звено решает.
type TextReport struct {
	Title      string     `json:"title"`
	Compliance CheckScore `json:"compliance"`
	Quality    CheckScore `json:"quality"`
	Overall    int        `json:"overall"`
	Verdict    string     `json:"verdict"`
}

// PassThreshold — оба измерения должны быть не ниже, чтобы текст прошёл.
const PassThreshold = 80

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
type ComplianceChecker struct{ llm llm.Client }

func NewComplianceChecker(c llm.Client) *ComplianceChecker { return &ComplianceChecker{llm: c} }

func (ch *ComplianceChecker) Run(ctx context.Context, briefText string, t TextToReview) (CheckScore, llm.Usage, error) {
	user := fmt.Sprintf("БРИФ:\n%s\n\nТЕКСТ ДЛЯ ПРОВЕРКИ:\nЗаголовок: %s\n\n%s",
		briefText, t.Title, t.Body)
	var out CheckScore
	usage, err := ch.llm.Complete(ctx, RoleCompliance, complianceSystem, user, &out)
	if err != nil {
		return CheckScore{}, usage, fmt.Errorf("compliance: %w", err)
	}
	out.Score = clampScore(out.Score)
	return out, usage, nil
}

// QualityChecker — агент «корректность текста».
type QualityChecker struct{ llm llm.Client }

func NewQualityChecker(c llm.Client) *QualityChecker { return &QualityChecker{llm: c} }

func (q *QualityChecker) Run(ctx context.Context, t TextToReview) (CheckScore, llm.Usage, error) {
	user := fmt.Sprintf("Заголовок: %s\n\n%s", t.Title, t.Body)
	var out CheckScore
	usage, err := q.llm.Complete(ctx, RoleQuality, qualitySystem, user, &out)
	if err != nil {
		return CheckScore{}, usage, fmt.Errorf("quality: %w", err)
	}
	out.Score = clampScore(out.Score)
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

// Verdict — проходное решение по одному тексту (оба измерения ≥ PassThreshold).
func Verdict(compliance, quality int) string {
	if compliance >= PassThreshold && quality >= PassThreshold {
		return "pass"
	}
	return "fix"
}
