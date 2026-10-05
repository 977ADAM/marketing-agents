package topicservice

import (
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

type Options struct {
	Wordstat                                            Source
	Semanticist                                         *Semanticist
	SeedCount, NumPhrases, MaxWordstatCalls, MaxPhrases int
	DefaultRegion                                       string
	Recorder                                            trace.Recorder
}
type Workflow struct {
	semanticist *Semanticist
	opt         Options
	trace       trace.Recorder
}

func NewWorkflow(c corellm.Client, opt Options) *Workflow {
	sem := opt.Semanticist
	if sem == nil {
		sem = NewSemanticist(c)
	}
	return &Workflow{sem, opt, trace.OrNop(opt.Recorder)}
}
