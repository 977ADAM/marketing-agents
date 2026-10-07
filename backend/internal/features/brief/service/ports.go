package briefservice

import (
	"context"

	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
)

// Streamer — то, что сервису нужно от вызова модели: ответ по фрагментам.
//
// Порт объявлен у потребителя и по форме совпадает с corellm.Streamer:
// реализации (стриминговый клиент и его декораторы) импортируют ядро, а сервис
// интервью не связывается с конкретным адаптером.
type Streamer interface {
	CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error)
}
