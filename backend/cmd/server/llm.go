package main

import (
	"github.com/977ADAM/marketing-agents/internal/adapters/accounting"
	"github.com/977ADAM/marketing-agents/internal/adapters/skills"
	tracing "github.com/977ADAM/marketing-agents/internal/adapters/tracing"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// newLLMClient собирает цепочку декораторов вызова модели: скилы — снаружи
// трассы, иначе в трассе останется промпт без скила; учёт расходов — крайний.
func newLLMClient(base corellm.Client, rec trace.Recorder) (corellm.Client, error) {
	skilled, err := skills.New(tracing.NewLLM(base, rec), skills.Options{Bindings: skillBindings()})
	if err != nil {
		return nil, err
	}
	return accounting.New(skilled), nil
}
