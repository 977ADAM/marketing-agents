package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// FakeClient возвращает заранее заданные JSON-ответы по роли и считает вызовы.
// Потокобезопасен: оркестратор вызывает копирайтеров из параллельных горутин.
type FakeClient struct {
	mu sync.Mutex
	// Responses: role -> очередь JSON-строк (по одной на вызов).
	Responses map[string][]string
	Calls     map[string]int
	Err       error
}

func NewFake() *FakeClient {
	return &FakeClient{Responses: map[string][]string{}, Calls: map[string]int{}}
}

func (f *FakeClient) Complete(_ context.Context, role, _, _ string, out any) (Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
