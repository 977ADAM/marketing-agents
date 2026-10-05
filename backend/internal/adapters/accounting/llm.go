package accounting

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/identity"
	llm "github.com/977ADAM/marketing-agents/internal/core/llm"
	"time"
)

type Client struct{ inner llm.Client }

func New(inner llm.Client) *Client { return &Client{inner} }
func (c *Client) Complete(ctx context.Context, role, system, user string, out any) (llm.Usage, error) {
	u, err := c.inner.Complete(ctx, role, system, user, out)
	entries := u.Entries
	if len(entries) == 0 {
		model := ""
		if m, ok := c.inner.(interface{ ModelFor(string) string }); ok {
			model = m.ModelFor(role)
		}
		entries = []llm.UsageEntry{{Model: model, Role: role, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}}
	}
	for i := range entries {
		if entries[i].ID == "" {
			entries[i].ID = identity.NewUUID()
		}
		if entries[i].Role == "" {
			entries[i].Role = role
		}
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		saveErr := llm.RecordUsage(final, entries[i])
		cancel()
		if saveErr != nil {
			return llm.Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, Entries: entries}, fmt.Errorf("persist usage: %w", saveErr)
		}
	}
	u.Entries = entries
	return u, err
}
func (c *Client) ModelFor(role string) string {
	if m, ok := c.inner.(interface{ ModelFor(string) string }); ok {
		return m.ModelFor(role)
	}
	return ""
}
