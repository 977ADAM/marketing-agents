package accounting

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/identity"
	llm "github.com/977ADAM/marketing-agents/internal/core/llm"
	"time"
)

type Client struct{ inner llm.Client }

// Декоратор обязан пробрасывать стрим: иначе утверждение типа на llm.Streamer
// в composition root не соберётся.
var _ llm.Streamer = (*Client)(nil)

func New(inner llm.Client) *Client { return &Client{inner} }
func (c *Client) Complete(ctx context.Context, role, system, user string, out any) (llm.Usage, error) {
	u, err := c.inner.Complete(ctx, role, system, user, out)
	return c.persist(ctx, role, u, err)
}

// CompleteStream повторяет Complete для потокового вызова: расход пишется тем же
// способом, а usage и ошибка уходят наверх без изменений.
func (c *Client) CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (llm.Usage, error) {
	streamer, ok := c.inner.(llm.Streamer)
	if !ok {
		return llm.Usage{}, fmt.Errorf("accounting: клиент не поддерживает стриминг")
	}
	u, err := streamer.CompleteStream(ctx, role, system, user, onDelta)
	return c.persist(ctx, role, u, err)
}

// persist записывает расход вызова: записи из Usage.Entries, а при их отсутствии
// — синтетическую запись с моделью из ModelFor(role). Роль и идентификатор
// дописываются здесь же, чтобы обе ветки — Complete и стрим — не расходились.
func (c *Client) persist(ctx context.Context, role string, u llm.Usage, err error) (llm.Usage, error) {
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
			u.Entries = entries
			return u, fmt.Errorf("persist usage: %w", saveErr)
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
