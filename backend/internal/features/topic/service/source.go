package topicservice

import (
	"context"
	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
)

// Source — порт источника спроса: то, что подбору тем нужно от поисковика.
//
// Объявлен здесь, у потребителя: реализации (Wordstat через MCP, Fake в тестах)
// импортируют домен, а домен про них не знает.
type Source interface {
	Demand(ctx context.Context, p topic.DemandParams) (topic.Demand, error)
	Dynamics(ctx context.Context, p topic.DynamicsParams) (topic.Dynamics, error)
}
