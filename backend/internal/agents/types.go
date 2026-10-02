// Package agents — LLM-агенты отдела контента и их типизированные I/O.
package agents

// Brief — вход пайплайна.
type Brief struct {
	Product  string `json:"product"`
	Goal     string `json:"goal"`
	Audience string `json:"audience"`
	Tone     string `json:"tone"`
}

// Topic — тема статьи, выданная стратегом.
type Topic struct {
	Title  string   `json:"title"`
	Angle  string   `json:"angle"`
	Points []string `json:"points"`
}

// Strategy — выход стратега.
type Strategy struct {
	Positioning string  `json:"positioning"`
	Topics      []Topic `json:"topics"`
	// TopicCandidates — все рассмотренные темы (их вдвое больше, чем статей) с
	// доказательствами: объёмом, цитатами запросов, сезонностью. В генерацию
	// уходят только Topics, а здесь остаётся всё, из чего выбирали.
	TopicCandidates []TopicCandidate `json:"topic_candidates,omitempty"`
	// WordstatCalls — сколько обращений к Wordstat потребовал подбор тем.
	WordstatCalls int `json:"wordstat_calls,omitempty"`
}

// Источники темы: подтверждённая спросом или придуманная моделью, когда спроса
// нет. Пользователь должен видеть разницу — у llm-тем цифр не показываем.
const (
	SourceWordstat = "wordstat"
	SourceLLM      = "llm"
)

// PhraseCount — фраза и её частотность: доказательство темы.
type PhraseCount struct {
	Phrase string `json:"phrase"`
	Count  int64  `json:"count"`
}

// Seasonality — сезонная поправка по данным за 12 месяцев. Нужна потому, что
// окно Wordstat — 30 дней: у сезонных тем спрос в межсезонье падает в разы
// (замерено до 8.9x), и без поправки живые темы отсекались бы порогом.
type Seasonality struct {
	Peak      int64   `json:"peak"`
	PeakMonth string  `json:"peak_month,omitempty"`
	Trough    int64   `json:"trough"`
	Ratio     float64 `json:"ratio"`
	Seasonal  bool    `json:"seasonal"`
}

// RegionShare — доля региона в спросе по теме.
type RegionShare struct {
	RegionID      string  `json:"region_id"`
	Name          string  `json:"name,omitempty"`
	Count         int64   `json:"count"`
	Share         float64 `json:"share"`
	AffinityIndex float64 `json:"affinity_index"`
}

// TopicCandidate — тема, рассмотренная при подборе.
//
// Volume — максимальная частотность среди цитат темы: это нижняя оценка спроса
// (не сумма формулировок, которая завышает в разы, потому что популярные
// запросы — подмножества широкой частотности).
type TopicCandidate struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Goal     string        `json:"goal"`
	Task     string        `json:"task"`
	Source   string        `json:"source"`
	Selected bool          `json:"selected"`
	Volume   int64         `json:"volume"`
	Head     string        `json:"head"`
	Queries  []PhraseCount `json:"queries"`
	Season   *Seasonality  `json:"season,omitempty"`
	Regions  []RegionShare `json:"regions,omitempty"`
	Intent   string        `json:"intent,omitempty"`
	// Reject — почему тема не пошла в генерацию (пусто — прошла отбор или
	// просто не хватило мест). Видно в результате, чтобы не было «её нет и
	// непонятно почему».
	Reject string `json:"reject,omitempty"`
}

// Article — выход копирайтера.
type Article struct {
	Topic string `json:"topic"`
	Title string `json:"title"`
	Body  string `json:"body"`
	CTA   string `json:"cta"`
}

// Review — выход критика. Verdict: "accept" | "revise".
// Severity — градация score для интерфейса (см. Severity()).
type Review struct {
	Score    int      `json:"score"`
	Issues   []string `json:"issues"`
	Verdict  string   `json:"verdict"`
	Severity string   `json:"severity"`
}

// Deliverable — статья с прикреплённым ревью (итог по теме).
type Deliverable struct {
	Article
	Review Review `json:"review"`
}
