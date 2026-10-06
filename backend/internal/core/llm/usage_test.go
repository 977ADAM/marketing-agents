package corellm_test

import (
	"testing"

	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
)

// Add складывает расход вызовов. Токены размышлений — часть completion, и при
// агрегации они не должны теряться.
func TestUsageAddSumsReasoningTokens(t *testing.T) {
	a := corellm.Usage{
		PromptTokens: 10, CompletionTokens: 20, ReasoningTokens: 15,
		Entries: []corellm.UsageEntry{{Model: "m", Role: "critic", PromptTokens: 10, CompletionTokens: 20}},
	}
	b := corellm.Usage{PromptTokens: 1, CompletionTokens: 2, ReasoningTokens: 2}

	sum := a.Add(b)
	if sum.PromptTokens != 11 || sum.CompletionTokens != 22 || sum.ReasoningTokens != 17 {
		t.Errorf("сумма = %+v, want 11/22/17", sum)
	}
	if len(sum.Entries) != 1 || sum.Entries[0].Model != "m" {
		t.Errorf("entries = %+v", sum.Entries)
	}
	// Тела одного вызова в агрегат не переносятся: это не расход, а содержимое.
	if sum.Response != "" || sum.Reasoning != "" {
		t.Errorf("тела не должны попадать в сумму: %+v", sum)
	}
}
