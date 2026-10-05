// Package review — домен проверки готовых текстов: два измерения (соответствие
// брифу и корректность текста), оценки и вердикт. Домен: не знает ни про HTTP,
// ни про SQL.
package review

import "github.com/977ADAM/marketing-agents/internal/score"

// TextToReview — готовая статья, которую проверяют агенты.
type TextToReview struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// CheckScore — оценка одного агента по одному измерению.
// Severity — градация score для интерфейса (см. score.Severity).
type CheckScore struct {
	Score    int      `json:"score"`
	Issues   []string `json:"issues"`
	Severity string   `json:"severity"`
}

// TextReport — итог проверки одного текста. Verdict: "pass" | "fix".
// Overall — min(compliance, quality): слабое звено решает;
// Severity — градация Overall для интерфейса.
type TextReport struct {
	Title      string     `json:"title"`
	Compliance CheckScore `json:"compliance"`
	Quality    CheckScore `json:"quality"`
	Overall    int        `json:"overall"`
	Verdict    string     `json:"verdict"`
	Severity   string     `json:"severity"`
}

// Request — вход проверки готовых текстов: бриф + тексты.
type Request struct {
	BriefText string         `json:"brief"`
	Texts     []TextToReview `json:"texts"`
}

// Result — итог проверки: отчёты по текстам, сводка и суммарная стоимость.
// Passed — сколько текстов прошло (verdict=pass): считает бэкенд, фронт только
// показывает готовое число.
type Result struct {
	Items   []TextReport `json:"items"`
	Passed  int          `json:"passed"`
	CostUSD float64      `json:"cost_usd"`
}

// Решения по одному тексту: pass — можно публиковать, fix — нужна доработка.
const (
	VerdictPass = "pass"
	VerdictFix  = "fix"
)

// Verdict — проходное решение по одному тексту (оба измерения ≥ PassThreshold).
func Verdict(compliance, quality int) string {
	if compliance >= score.PassThreshold && quality >= score.PassThreshold {
		return VerdictPass
	}
	return VerdictFix
}
