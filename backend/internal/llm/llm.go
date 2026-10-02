// Package llm — обёртка над DeepSeek (OpenAI-совместимый) API.
package llm

import "context"

// Usage — потокены одного вызова, для подсчёта стоимости.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

func (u Usage) Add(o Usage) Usage {
	return Usage{u.PromptTokens + o.PromptTokens, u.CompletionTokens + o.CompletionTokens}
}

// Client — то, что нужно агентам: один вызов с JSON-ответом, разобранным в out.
// role задаёт модель (через маршрутизацию). system/user — промпты.
type Client interface {
	Complete(ctx context.Context, role, system, user string, out any) (Usage, error)
}
