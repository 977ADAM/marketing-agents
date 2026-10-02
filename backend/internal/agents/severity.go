package agents

// Градация оценки для интерфейса. Пороги живут только на бэкенде: фронт
// отображает пришедшее значение и ничего не считает сам.
const (
	SeverityGood = "good" // оценка ≥ PassThreshold
	SeverityWarn = "warn" // 60…PassThreshold-1
	SeverityBad  = "bad"  // ниже 60
)

// Severity переводит оценку 0–100 в градацию для UI.
func Severity(score int) string {
	switch {
	case score >= PassThreshold:
		return SeverityGood
	case score >= 60:
		return SeverityWarn
	default:
		return SeverityBad
	}
}
