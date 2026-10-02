package llm

import (
	"context"
	"encoding/json"
	"fmt"
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

// FakeClient возвращает заранее заданные JSON-ответы по роли и считает вызовы.
// Потокобезопасен: оркестратор вызывает копирайтеров из параллельных горутин.
type FakeClient struct {
	mu sync.Mutex
	// Responses: role -> очередь JSON-строк (по одной на вызов).
	Responses map[string][]string
	Calls     map[string]int
	// Requests — журнал вызовов в порядке обращения.
	Requests []Request
	Err      error
}

func NewFake() *FakeClient {
	return &FakeClient{Responses: map[string][]string{}, Calls: map[string]int{}}
}

func (f *FakeClient) Complete(_ context.Context, role, system, user string, out any) (Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, Request{Role: role, System: system, User: user})
	if f.Err != nil {
		return Usage{}, f.Err
	}
	queue := f.Responses[role]
	n := f.Calls[role]
	if n >= len(queue) {
		return Usage{}, fmt.Errorf("fake: no response for role %q call #%d", role, n)
	}
	f.Calls[role]++
	if err := json.Unmarshal([]byte(queue[n]), out); err != nil {
		return Usage{}, err
	}
	return Usage{PromptTokens: 10, CompletionTokens: 10}, nil
}

// LastRequest возвращает последний зафиксированный вызов.
func (f *FakeClient) LastRequest() (Request, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Requests) == 0 {
		return Request{}, false
	}
	return f.Requests[len(f.Requests)-1], true
}
