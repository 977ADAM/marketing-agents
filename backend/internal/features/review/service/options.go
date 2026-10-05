package reviewservice

import run "github.com/977ADAM/marketing-agents/internal/core/run"
import corellm "github.com/977ADAM/marketing-agents/internal/core/llm"

type Options struct {
	Prices                               *corellm.Prices
	Checkpoints                          run.CheckpointStore
	CostPer1KPrompt, CostPer1KCompletion float64
	ParallelTexts                        int
}
type Workflow struct {
	llm corellm.Client
	opt Options
}

func NewWorkflow(c corellm.Client, opt Options) *Workflow { return &Workflow{c, opt} }
func (o *Workflow) cost(u corellm.Usage) float64 {
	return float64(u.PromptTokens)/1000*o.opt.CostPer1KPrompt + float64(u.CompletionTokens)/1000*o.opt.CostPer1KCompletion
}
