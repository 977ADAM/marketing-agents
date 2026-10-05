package corellm

import "testing"

func TestPricesByModel(t *testing.T) {
	p := Prices{Models: map[string]Rates{"strong": {Prompt: 2, Completion: 4}, "fast": {Prompt: 1, Completion: 2}}}
	amount, known := p.Estimate([]UsageEntry{{Model: "strong", PromptTokens: 1000, CompletionTokens: 1000}, {Model: "fast", PromptTokens: 1000, CompletionTokens: 1000}})
	if !known || amount != 9 {
		t.Fatalf("amount=%v known=%v", amount, known)
	}
	_, known = p.Estimate([]UsageEntry{{Model: "unknown", PromptTokens: 10}})
	if known {
		t.Fatal("unknown model treated as free")
	}
}
