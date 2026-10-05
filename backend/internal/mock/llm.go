// Package mock — тест-двойники портов: держим их вне пакетов-продюсеров, чтобы
// прод-код не тащил тестовые сущности в бинарь (см. Go Code Review Comments:
// «Do not define interfaces on the implementor side of an API for mocking»).
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/corellm"
	"sync"
)

// Request — один зафиксированный вызов: роль и промпты. Нужен тестам, которые
// проверяют не только разбор ответа, но и то, что уходит модели (например, что
// числа из Wordstat в промпт не попадают).
type Request struct {
	Role   string
	System string
	User   string
}

// LLM возвращает заранее заданные JSON-ответы по роли и считает вызовы.
// Потокобезопасен: оркестратор вызывает копирайтеров из параллельных горутин.
type LLM struct {
	mu sync.Mutex
	// Responses: role -> очередь JSON-строк (по одной на вызов).
	Responses map[string][]string
	Calls     map[string]int
	// Requests — журнал вызовов в порядке обращения.
	Requests []Request
	Err      error
}

func NewLLM() *LLM {
	return &LLM{Responses: map[string][]string{}, Calls: map[string]int{}}
}

func (f *LLM) Complete(_ context.Context, role, system, user string, out any) (corellm.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, Request{Role: role, System: system, User: user})
	if f.Err != nil {
		return corellm.Usage{}, f.Err
	}
	queue := f.Responses[role]
	n := f.Calls[role]
	if n >= len(queue) {
		return corellm.Usage{}, fmt.Errorf("fake: no response for role %q call #%d", role, n)
	}
	f.Calls[role]++
	if err := json.Unmarshal([]byte(queue[n]), out); err != nil {
		return corellm.Usage{}, err
	}
	return corellm.Usage{PromptTokens: 10, CompletionTokens: 10}, nil
}

// LastRequest возвращает последний зафиксированный вызов.
func (f *LLM) LastRequest() (Request, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Requests) == 0 {
		return Request{}, false
	}
	return f.Requests[len(f.Requests)-1], true
}
