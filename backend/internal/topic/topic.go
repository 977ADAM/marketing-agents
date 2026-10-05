// Package topic — подбор тем по поисковому спросу: типы, порт источника спроса и
// правила отбора. Домен: не знает ни про HTTP, ни про SQL, ни про MCP.
package topic

import ()

// Briefing — проекция брифа, нужная подбору тем: продукт, цель, аудитория, тон.
//
// Сам бриф живёт в домене кампании; если бы подбор принимал его напрямую, а
// стратегия кампании ссылалась на темы из этого пакета, получился бы цикл
// импортов campaign ↔ topic. Поэтому у подбора свой вход.
type Briefing struct {
	Product  string
	Goal     string
	Audience string
	Tone     string
}

// Источники темы: подтверждённая спросом или придуманная моделью, когда спроса
// нет. Пользователь должен видеть разницу — у llm-тем цифр не показываем.
const (
	SourceWordstat = "wordstat"
	SourceLLM      = "llm"
)

// TopicDraft — тема-кандидат от модели. Чисел здесь нет и быть не может:
// объём, сезонность и источник дописывает код (см. TopicCandidate).
type TopicDraft struct {
	Title   string   `json:"title"`
	Goal    string   `json:"goal"`
	Task    string   `json:"task"`
	Queries []string `json:"queries"`
	Intent  string   `json:"intent,omitempty"`
}

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
