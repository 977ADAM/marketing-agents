package wordstat

import (
	"context"
	"fmt"
	"sync"
)

// Fake — подмена Source в тестах: заранее заданные ответы по фразам плюс журнал
// вызовов. По умолчанию (фраза не описана) возвращает «спроса нет» — это
// валидный ответ, а не ошибка, поэтому тесты не обязаны описывать каждую фразу.
type Fake struct {
	mu sync.Mutex

	// Tops — ответы top_requests по фразе.
	Tops map[string]*Top
	// Default — ответ для фраз, которых нет в Tops.
	Default *Top
	// DynamicsR и RegionsR — ответы соответствующих инструментов.
	DynamicsR *Dynamics
	RegionsR  *Regions
	// Err — если задан, все вызовы возвращают эту ошибку.
	Err error

	calls []string
	// TopParamsLog — параметры запросов спроса: тесты проверяют, что регион и
	// число фраз действительно уходят в источник.
	TopParamsLog []TopParams
	// DynamicsParamsLog — параметры запросов сезонности.
	DynamicsParamsLog []DynamicsParams
}

// NewFake создаёт подмену с пустым журналом.
func NewFake() *Fake {
	return &Fake{Tops: map[string]*Top{}}
}

// SetTop описывает ответ top_requests по конкретной фразе.
func (f *Fake) SetTop(phrase string, top *Top) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Tops[phrase] = top
}

// TopRequests возвращает спрос по фразе.
func (f *Fake) TopRequests(_ context.Context, p TopParams) (*Top, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "top_requests:"+p.Phrase)
	f.TopParamsLog = append(f.TopParamsLog, p)
	if f.Err != nil {
		return nil, f.Err
	}
	if top, ok := f.Tops[p.Phrase]; ok {
		return top, nil
	}
	if f.Default != nil {
		return f.Default, nil
	}
	return &Top{Phrase: p.Phrase, HasData: false}, nil
}

// Dynamics возвращает сезонность.
func (f *Fake) Dynamics(_ context.Context, p DynamicsParams) (*Dynamics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "dynamics:"+p.Phrase)
	f.DynamicsParamsLog = append(f.DynamicsParamsLog, p)
	if f.Err != nil {
		return nil, f.Err
	}
	if f.DynamicsR != nil {
		return f.DynamicsR, nil
	}
	return &Dynamics{Phrase: p.Phrase, Period: p.Period}, nil
}

// Regions возвращает географию.
func (f *Fake) Regions(_ context.Context, p RegionsParams) (*Regions, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "regions:"+p.Phrase)
	if f.Err != nil {
		return nil, f.Err
	}
	if f.RegionsR != nil {
		return f.RegionsR, nil
	}
	return &Regions{Phrase: p.Phrase}, nil
}

// Calls возвращает журнал вызовов в формате «инструмент:фраза».
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// CallCount — число обращений к источнику (для проверки лимита вызовов).
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// Seed — описание спроса для тестов: объём, формулировки и ассоциации.
func Seed(phrase string, total int64, requests map[string]int64) *Top {
	top := &Top{Phrase: phrase, TotalCount: total, HasData: total > 0}
	for text, count := range requests {
		top.Requests = append(top.Requests, PhraseCount{Phrase: text, Count: count})
	}
	return top
}

// String — для читаемых сообщений об ошибках в тестах.
func (f *Fake) String() string {
	return fmt.Sprintf("wordstat.Fake(%d вызовов)", f.CallCount())
}
