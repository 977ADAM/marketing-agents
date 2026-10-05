package corellm

import (
	"encoding/json"
	"fmt"
	"math"
)

type UsageEntry struct {
	ID               string `json:"id"`
	Model            string `json:"model"`
	Role             string `json:"role"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}
type Rates struct {
	Prompt     float64 `json:"prompt_per_1k"`
	Completion float64 `json:"completion_per_1k"`
}
type Prices struct {
	Models map[string]Rates
	Legacy *Rates
}

func ParsePrices(raw string, legacy Rates) (Prices, error) {
	if raw == "" {
		return Prices{Legacy: &legacy}, nil
	}
	var models map[string]Rates
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return Prices{}, fmt.Errorf("MODEL_PRICES_JSON: invalid JSON")
	}
	if len(models) == 0 {
		return Prices{}, fmt.Errorf("MODEL_PRICES_JSON: model rates are required")
	}
	for model, r := range models {
		if model == "" || r.Prompt < 0 || r.Completion < 0 || math.IsNaN(r.Prompt) || math.IsNaN(r.Completion) || math.IsInf(r.Prompt, 0) || math.IsInf(r.Completion, 0) {
			return Prices{}, fmt.Errorf("MODEL_PRICES_JSON: invalid model rates")
		}
	}
	return Prices{Models: models}, nil
}
func (p Prices) Estimate(entries []UsageEntry) (float64, bool) {
	total := 0.0
	known := true
	for _, e := range entries {
		r, ok := p.Models[e.Model]
		if !ok && len(p.Models) == 0 && p.Legacy != nil {
			r = *p.Legacy
			ok = true
		}
		if !ok {
			known = false
			continue
		}
		total += float64(e.PromptTokens)/1000*r.Prompt + float64(e.CompletionTokens)/1000*r.Completion
	}
	return total, known
}
func FromEntries(entries []UsageEntry) Usage {
	u := Usage{Entries: append([]UsageEntry(nil), entries...)}
	for _, e := range entries {
		u.PromptTokens += e.PromptTokens
		u.CompletionTokens += e.CompletionTokens
	}
	return u
}
func EstimateUsage(u Usage, p *Prices, legacy Rates) (float64, bool) {
	if p == nil {
		return float64(u.PromptTokens)/1000*legacy.Prompt + float64(u.CompletionTokens)/1000*legacy.Completion, true
	}
	entries := u.Entries
	if len(entries) == 0 && (u.PromptTokens > 0 || u.CompletionTokens > 0) {
		entries = []UsageEntry{{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}}
	}
	return p.Estimate(entries)
}
