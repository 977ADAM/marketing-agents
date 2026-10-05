// Package score — градация оценок 0–100 для интерфейса.
//
// Общий kernel домена: градация нужна и критику статей (campaign), и проверке
// готовых текстов (review). Пороги живут только на бэкенде — фронт отображает
// пришедшее значение и ничего не считает сам.
package score

import ()

import ()

const (
	// PassThreshold — оценка, с которой результат считается проходным.
	PassThreshold = 80
	// Good — оценка ≥ PassThreshold, Warn — 60…PassThreshold-1, Bad — ниже 60.
	Good = "good"
	Warn = "warn"
	Bad  = "bad"
)

// Severity переводит оценку 0–100 в градацию для UI.
func Severity(score int) string {
	switch {
	case score >= PassThreshold:
		return Good
	case score >= 60:
		return Warn
	default:
		return Bad
	}
}
