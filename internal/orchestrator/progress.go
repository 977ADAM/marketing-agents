package orchestrator

// Phase — крупная стадия прогона.
type Phase string

const (
	PhaseStrategizing Phase = "strategizing"
	PhaseProducing    Phase = "producing"
	PhaseDone         Phase = "done"
	PhaseFailed       Phase = "failed"
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
	Phase      Phase           `json:"phase"`
	Topics     []TopicProgress `json:"topics"`
	TopicTotal int             `json:"topic_total"`
	TopicsDone int             `json:"topics_done"`
	Percent    int             `json:"percent"`
}

// Progress — оркестратор «объявляет», что делает. Реализация concurrency-safe
// (темы обрабатываются параллельно).
type Progress interface {
	Strategizing()
	TopicsPlanned(titles []string)
	TopicWriting(i int)
	TopicReviewing(i, iter int)
	TopicRevising(i, iter int)
	TopicDone(i, score int)
}

// NopProgress — заглушка по умолчанию (для тестов и nil-вызовов).
type NopProgress struct{}

func (NopProgress) Strategizing()           {}
func (NopProgress) TopicsPlanned([]string)  {}
func (NopProgress) TopicWriting(int)        {}
func (NopProgress) TopicReviewing(int, int) {}
func (NopProgress) TopicRevising(int, int)  {}
func (NopProgress) TopicDone(int, int)      {}

// Доли прогресс-бара (настраиваемые).
const (
	pctStrategizing = 5
	pctPlanned      = 10
	pctProducingMax = 95
)

// computePercent — оценка % по фазе и числу готовых тем. Для PhaseFailed
// процент не вычисляется (вызывающая сторона не трогает прошлое значение).
func computePercent(ph Phase, done, total int) int {
	switch ph {
	case PhaseStrategizing:
		return pctStrategizing
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
