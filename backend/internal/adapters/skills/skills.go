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

// proseCaveat — оговорка для роли, которая отвечает прозой (интервьюер): скил
// описывает содержание, но ответ — свободный текст, а не JSON по схеме.
const proseCaveat = `Файлы не создаются: раздел «Результат: …» выше описывает требуемое
содержание, а не файл на диске. Формат ответа задан ниже и важнее инструкций выше.`

// Options настраивает декоратор: Bindings связывает роль с именем скила, Prose
// отмечает роли, которые отвечают прозой, а не JSON.
type Options struct {
	// Bindings — роль вызова → имя встроенного скила.
	Bindings map[string]string
	// Prose — роли, для которых стрим отвечает прозой: им вместо контракта JSON
	// подставляется короткая прозаическая оговорка.
	Prose map[string]bool
}

// Client — декоратор corellm.Client: для роли с байнда подставляет текст скила
// в начало system-промпта, остальные роли пропускает без изменений.
type Client struct {
	inner   corellm.Client
	prompts map[string]string // роль → текст скила
	prose   map[string]bool   // роль → отвечает прозой
}

// Декоратор обязан пробрасывать стрим: иначе утверждение типа на corellm.Streamer
// в composition root не соберётся.
var _ corellm.Streamer = (*Client)(nil)

// New создаёт декоратор поверх inner и заранее читает тексты всех скилов, чтобы
// ошибка конфигурации всплыла при старте, а не на первом вызове модели.
func New(inner corellm.Client, opts Options) (*Client, error) {
	if inner == nil {
		return nil, fmt.Errorf("skills: внутренний клиент не задан")
	}
	c := &Client{
		inner:   inner,
		prompts: make(map[string]string, len(opts.Bindings)),
		prose:   make(map[string]bool, len(opts.Prose)),
	}
	for role, isProse := range opts.Prose {
		c.prose[role] = isProse
	}
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
		system = composeSystem(text, contractCaveat, system)
	}
	return c.inner.Complete(ctx, role, system, user, out)
}

// CompleteStream повторяет Complete для потокового вызова: прозаической роли
// вместо контракта JSON подставляется короткая оговорка, остальным — прежняя.
// onDelta уходит внутреннему клиенту без изменений, как и usage с ошибкой.
func (c *Client) CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error) {
	if text, ok := c.prompts[role]; ok {
		caveat := contractCaveat
		if c.prose[role] {
			caveat = proseCaveat
		}
		system = composeSystem(text, caveat, system)
	}
	streamer, ok := c.inner.(corellm.Streamer)
	if !ok {
		return corellm.Usage{}, fmt.Errorf("skills: клиент не поддерживает стриминг")
	}
	return streamer.CompleteStream(ctx, role, system, user, onDelta)
}

// composeSystem собирает system из трёх частей: текст скила, оговорка о формате,
// контракт роли. Порядок общий для Complete и CompleteStream.
func composeSystem(skill, caveat, system string) string {
	return skill + "\n\n" + caveat + "\n\n" + system
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
