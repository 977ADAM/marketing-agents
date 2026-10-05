// Package corellm — порт вызова модели: то, что нужно агенту от поставщика.
//
// Ядро (домен и сценарии) зависит только от этого интерфейса, а конкретный
// клиент — DeepSeek через OpenAI-совместимый API — живёт адаптером в internal/llm
// вместе с ретраями и декоратором трассы.
package corellm

import "context"

// Usage — токены одного вызова, для подсчёта стоимости.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	Response         string `json:"-"`
	Entries          []UsageEntry
}

// Add складывает расход двух вызовов.
func (u Usage) Add(o Usage) Usage {
	return Usage{PromptTokens: u.PromptTokens + o.PromptTokens, CompletionTokens: u.CompletionTokens + o.CompletionTokens, Entries: append(append([]UsageEntry(nil), u.Entries...), o.Entries...)}
}

// Client — один вызов с JSON-ответом, разобранным в out. role задаёт модель
// (через маршрутизацию). system/user — промпты.
type Client interface {
	Complete(ctx context.Context, role, system, user string, out any) (Usage, error)
}
