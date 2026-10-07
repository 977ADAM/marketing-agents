package skills

import (
	"context"
	"fmt"
	"sort"

	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
)

// contractCaveat — оговорка, которую декоратор ставит последней в system-промпте:
// скил описывает содержание и структуру ответа, но формат задаёт сервис.
const contractCaveat = `Формат ответа этого сервиса важнее инструкций выше.
Ответ — только JSON по схеме, без markdown-обёрток, файлов и текста вне JSON.
Файлы не создаются: раздел «Результат: …» выше описывает требуемое содержание
и структуру, а не файл на диске. Служебные разделы скила (метаданные, реестры
утверждений и замечаний, недостающие данные) в ответ не выносятся и новых ключей
JSON не добавляют. Там, где скил требует замечание с важностью, цитатой,
нарушенным критерием, требуемой правкой и критерием закрытия, всё это помещается
в строку соответствующего поля JSON. Если скил предлагает значение, которого нет
в схеме (например verdict "needs_input"), используй ближайшее допустимое
("revise"). Оценка и вердикт проверяются кодом: отсутствующее или недопустимое
значение — ошибка.`

// Options настраивает декоратор: Bindings связывает роль с именем скила.
type Options struct {
	// Bindings — роль вызова → имя встроенного скила.
	Bindings map[string]string
}

// Client — декоратор corellm.Client: для роли с байнда подставляет текст скила
// в начало system-промпта, остальные роли пропускает без изменений.
type Client struct {
	inner   corellm.Client
	prompts map[string]string // роль → текст скила
}

// New создаёт декоратор поверх inner и заранее читает тексты всех скилов, чтобы
// ошибка конфигурации всплыла при старте, а не на первом вызове модели.
func New(inner corellm.Client, opts Options) (*Client, error) {
	if inner == nil {
		return nil, fmt.Errorf("skills: внутренний клиент не задан")
	}
	c := &Client{inner: inner, prompts: make(map[string]string, len(opts.Bindings))}
	// Роли обходим по порядку: при нескольких плохих байндах ошибка стабильна.
	roles := make([]string, 0, len(opts.Bindings))
	for role := range opts.Bindings {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		text, err := Prompt(opts.Bindings[role])
		if err != nil {
			return nil, fmt.Errorf("skills: роль %q: %w", role, err)
		}
		c.prompts[role] = text
	}
	return c, nil
}

// Complete вызывает inner, подставив текст скила и оговорку о формате перед
// контрактом роли. Роль без байнда уходит как есть; usage, out и ошибка —
// без изменений.
func (c *Client) Complete(ctx context.Context, role, system, user string, out any) (corellm.Usage, error) {
	if text, ok := c.prompts[role]; ok {
		system = text + "\n\n" + contractCaveat + "\n\n" + system
	}
	return c.inner.Complete(ctx, role, system, user, out)
}

// modelNamer — необязательная возможность клиента сообщить модель роли.
type modelNamer interface {
	ModelFor(role string) string
}

// ModelFor повторяет необязательный интерфейс внутреннего клиента: учёт расходов
// выше по цепочке спрашивает модель у декоратора, и без проброса имя модели
// терялось бы в синтетической записи usage. Клиент без ModelFor даёт "".
func (c *Client) ModelFor(role string) string {
	if n, ok := c.inner.(modelNamer); ok {
		return n.ModelFor(role)
	}
	return ""
}
