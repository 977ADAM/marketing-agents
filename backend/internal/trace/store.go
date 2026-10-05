package trace

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound — события с таким seq у прогона нет.
var ErrNotFound = errors.New("event not found")

// Row — строка ленты событий прогона: то, что читает транспорт для вьюера трассы.
// Payload заполняется только в режиме full; HasPayload говорит, есть ли он.
type Row struct {
	Seq              int64
	At               time.Time
	Kind             string
	Name             string
	Status           string
	Summary          string
	DurationMS       int64
	PromptTokens     int
	CompletionTokens int
	Payload          string
	HasPayload       bool
	Error            string
}

// Store — чтение ленты событий (порт у потребителя; реализация — адаптер).
// Запись идёт через Sink, поэтому здесь только чтение.
type Store interface {
	RunEvents(ctx context.Context, runID string, limit int) ([]Row, error)
	RunEvent(ctx context.Context, runID string, seq int64) (*Row, error)
}
