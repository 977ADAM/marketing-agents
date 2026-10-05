// Package limits defines request and per-run budgets shared by transports and services.
package limits

import "fmt"

type Limits struct {
	MaxJSONBytes                                     int64
	MaxTexts, MaxTextBytes, MaxTopics, ParallelTexts int
}

func Defaults() Limits { return Limits{2 << 20, 20, 512 << 10, 5, 4} }
func Normalize(l Limits) Limits {
	d := Defaults()
	if l.MaxJSONBytes <= 0 {
		l.MaxJSONBytes = d.MaxJSONBytes
	}
	if l.MaxTexts <= 0 {
		l.MaxTexts = d.MaxTexts
	}
	if l.MaxTextBytes <= 0 {
		l.MaxTextBytes = d.MaxTextBytes
	}
	if l.MaxTopics <= 0 {
		l.MaxTopics = d.MaxTopics
	}
	if l.ParallelTexts <= 0 {
		l.ParallelTexts = d.ParallelTexts
	}
	return l
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string       { return e.Message }
func Invalid(format string, args ...any) error { return &ValidationError{fmt.Sprintf(format, args...)} }
