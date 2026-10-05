package mock

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
	"sync"

	"github.com/977ADAM/marketing-agents/internal/topic"
)

// Wordstat — подмена topic.Source в тестах: заранее заданные ответы по фразам плюс
// журнал вызовов. По умолчанию (фраза не описана) возвращает «спроса нет» — это
// валидный ответ, а не ошибка, поэтому тесты не обязаны описывать каждую фразу.
type Wordstat struct {
	mu sync.Mutex

	// Tops — ответы по спросу на фразу.
	Tops map[string]topic.Demand
	// Default — ответ для фраз, которых нет в Tops.
	Default *topic.Demand
	// DynamicsR и RegionsR — ответы соответствующих запросов.
	DynamicsR *topic.Dynamics
	RegionsR  *wordstat.Regions
	// Err — если задан, все вызовы возвращают эту ошибку.
	Err error

	calls []string
	// TopParamsLog — параметры запросов спроса: тесты проверяют, что регион и
	// число фраз действительно уходят в источник.
	TopParamsLog []topic.DemandParams
	// DynamicsParamsLog — параметры запросов сезонности.
	DynamicsParamsLog []topic.DynamicsParams
}

// NewWordstat создаёт подмену с пустым журналом.
func NewWordstat() *Wordstat {
	return &Wordstat{Tops: map[string]topic.Demand{}}
}

// SetTop описывает ответ по спросу на конкретную фразу.
func (f *Wordstat) SetTop(phrase string, demand topic.Demand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Tops[phrase] = demand
}

// Demand возвращает спрос по фразе (порт topic.Source).
func (f *Wordstat) Demand(_ context.Context, p topic.DemandParams) (topic.Demand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "top_requests:"+p.Phrase)
	f.TopParamsLog = append(f.TopParamsLog, p)
	if f.Err != nil {
		return topic.Demand{}, f.Err
	}
	if top, ok := f.Tops[p.Phrase]; ok {
		return top, nil
	}
	if f.Default != nil {
		return *f.Default, nil
	}
	return topic.Demand{Phrase: p.Phrase, HasData: false}, nil
}

// Dynamics возвращает сезонность (порт topic.Source).
func (f *Wordstat) Dynamics(_ context.Context, p topic.DynamicsParams) (topic.Dynamics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "dynamics:"+p.Phrase)
	f.DynamicsParamsLog = append(f.DynamicsParamsLog, p)
	if f.Err != nil {
		return topic.Dynamics{}, f.Err
	}
	if f.DynamicsR != nil {
		return *f.DynamicsR, nil
	}
	return topic.Dynamics{Phrase: p.Phrase, Period: p.Period}, nil
}

// Regions возвращает географию: в порт topic.Source не входит, но клиент и
// подмена её умеют — этим пользуются тесты декоратора и дымовой тест.
func (f *Wordstat) Regions(_ context.Context, p wordstat.RegionsParams) (*wordstat.Regions, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "regions:"+p.Phrase)
	if f.Err != nil {
		return nil, f.Err
	}
	if f.RegionsR != nil {
		return f.RegionsR, nil
	}
	return &wordstat.Regions{Phrase: p.Phrase}, nil
}

// Calls возвращает журнал вызовов в формате «инструмент:фраза».
func (f *Wordstat) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// CallCount — число обращений к источнику (для проверки лимита вызовов).
func (f *Wordstat) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// Seed — описание спроса для тестов: объём, формулировки и ассоциации.
func Seed(phrase string, total int64, requests map[string]int64) topic.Demand {
	demand := topic.Demand{Phrase: phrase, TotalCount: total, HasData: total > 0}
	for text, count := range requests {
		demand.Requests = append(demand.Requests, topic.PhraseCount{Phrase: text, Count: count})
	}
	return demand
}

// String — для читаемых сообщениях об ошибках в тестах.
func (f *Wordstat) String() string {
	return fmt.Sprintf("wordstat.Fake(%d вызовов)", f.CallCount())
}
