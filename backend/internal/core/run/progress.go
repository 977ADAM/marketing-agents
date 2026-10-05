// Package run — состояние прогона: фазы, подэтапы, снимок прогресса и порты,
// через которые оркестратор сообщает о ходе работы.
//
// Пакет-лист без зависимостей: его типы используют и домен (оркестратор), и
// адаптеры (MariaDB хранит снимок, HTTP отдаёт его в SSE).
package run

// Phase — крупная стадия прогона.
type Phase string

const (
	PhasePending      Phase = "pending"
	PhaseStrategizing Phase = "strategizing"
	PhaseResearching  Phase = "researching"
	PhaseProducing    Phase = "producing"
	PhaseDone         Phase = "done"
	PhaseFailed       Phase = "failed"
)

// ResearchStage — подэтап подбора тем по поисковому спросу.
type ResearchStage string

const (
	// StageSeeds — модель предлагает сеялки по брифу.
	StageSeeds ResearchStage = "seeds"
	// StageFetching — сбор спроса по сеялкам в Wordstat.
	StageFetching ResearchStage = "fetching"
	// StageSelecting — отбор лучших тем и сезонная поправка.
	StageSelecting ResearchStage = "selecting"
)

// TopicState — состояние работы над одной темой.
type TopicState string

const (
	TopicPending   TopicState = "pending"
	TopicWriting   TopicState = "writing"
	TopicReviewing TopicState = "reviewing"
	TopicRevising  TopicState = "revising"
	TopicDone      TopicState = "done"
)

// TopicProgress — прогресс по одной теме.
type TopicProgress struct {
	Index int        `json:"index"`
	Title string     `json:"title"`
	State TopicState `json:"state"`
	Iter  int        `json:"iter,omitempty"`  // 1-based итерация критика
	Score int        `json:"score,omitempty"` // последний/финальный score
}

// Snapshot — полный снимок прогресса прогона.
type Snapshot struct {
	Revision   int64           `json:"revision"`
	Phase      Phase           `json:"phase"`
	Topics     []TopicProgress `json:"topics"`
	TopicTotal int             `json:"topic_total"`
	TopicsDone int             `json:"topics_done"`
	Percent    int             `json:"percent"`
	// Stage — подэтап фазы researching (seeds, fetching, clustering, selecting).
	Stage string `json:"stage,omitempty"`
}

// Progress — оркестратор «объявляет», что делает. Реализация concurrency-safe
// (темы обрабатываются параллельно).
type Progress interface {
	Strategizing()
	TopicsPlanned(titles []string)
	TopicWriting(i int)
	TopicReviewing(i, iter int)
	// TopicRevising: iter — номер ревью (1-based), в ответ на которое идёт доработка.
	TopicRevising(i, iter int)
	TopicDone(i, score int)
}

// ResearchProgress — необязательная часть прогресса: события подбора тем.
// Реализации, которым нечего показывать (заглушки, простые recorder'ы в тестах),
// могут её не реализовывать — оркестратор просто не будет их пушить.
type ResearchProgress interface {
	// Researching объявляет текущий подэтап подбора.
	Researching(stage ResearchStage)
	// ResearchSeeds объявляет сеялки как единицы работы: дальше по каждой
	// приходит ResearchSeedDone.
	ResearchSeeds(seeds []string)
	// ResearchSeedDone отмечает, что спрос по сеялке собран.
	ResearchSeedDone(i int)
}

// NopProgress — заглушка по умолчанию (для тестов и nil-вызовов).
type NopProgress struct{}

func (NopProgress) Strategizing()           {}
func (NopProgress) TopicsPlanned([]string)  {}
func (NopProgress) TopicWriting(int)        {}
func (NopProgress) TopicReviewing(int, int) {}
func (NopProgress) TopicRevising(int, int)  {}
func (NopProgress) TopicDone(int, int)      {}

// NopProgress реализует и ResearchProgress — чтобы заглушка была полной.
func (NopProgress) Researching(ResearchStage) {}
func (NopProgress) ResearchSeeds([]string)    {}
func (NopProgress) ResearchSeedDone(int)      {}

// Доли прогресс-бара (настраиваемые).
const (
	pctStrategizing = 5
	pctPlanned      = 10
	pctProducingMax = 95
)

// Percent — публичная обёртка над computePercent для внешних потребителей (httpapi).
func Percent(ph Phase, done, total int) int { return computePercent(ph, done, total) }

// computePercent — оценка % по фазе и числу готовых тем. Для PhaseFailed
// процент не вычисляется (вызывающая сторона не трогает прошлое значение).
func computePercent(ph Phase, done, total int) int {
	switch ph {
	case PhaseStrategizing:
		// Позиционирование идёт после подбора тем (10% уже набраны) или вместо
		// него, когда подбор выключен. Держим 10%, чтобы полоса не откатывалась
		// назад при переходе от researching к strategizing.
		return pctPlanned
	case PhaseResearching:
		// Диапазон стратегии: 5% на старте и до 10% по мере сбора спроса.
		if total == 0 {
			return pctStrategizing
		}
		return pctStrategizing + (pctPlanned-pctStrategizing)*done/total
	case PhaseProducing:
		if total == 0 {
			return pctPlanned
		}
		return pctPlanned + (pctProducingMax-pctPlanned)*done/total
	case PhaseDone:
		return 100
	}
	return 0
}
