// Package wordstat — клиент Wordstat (спрос по поисковым фразам: частотность,
// сезонность, география) через MCP-сервер yandex-wordstat-mcp.
//
// Источник данных выбран в спеке: docs/superpowers/specs/2026-10-03-wordstat-semanticist-design.md.
// Числа приходят из structuredContent инструментов — текстовая форма ответа не
// разбирается намеренно: парсить человекочитаемый отчёт хрупко.
package wordstat

// PhraseCount — фраза и её частотность за последние 30 дней.
type PhraseCount struct {
	Phrase string `json:"phrase"`
	Count  int64  `json:"count"`
}

// Top — результат инструмента top_requests.
//
// TotalCount — «широкая» частотность головной фразы (запросы, содержащие все
// слова, в любом порядке) и единственная корректная мера объёма темы.
// Requests — популярные формулировки, и они являются подмножествами TotalCount,
// поэтому суммировать их нельзя (замерено: превышение в 3.4–5.5 раз).
// Associations — похожие запросы, они в подмножество не входят и часто шумят.
type Top struct {
	Phrase       string        `json:"phrase"`
	TotalCount   int64         `json:"totalCount"`
	Requests     []PhraseCount `json:"requests"`
	Associations []PhraseCount `json:"associations"`
	HasData      bool          `json:"hasData"`
	CacheHit     bool          `json:"cacheHit"`
	Regions      []string      `json:"regions"`
	Devices      []string      `json:"devices"`
}

// DynamicsPoint — точка временного ряда сезонности.
type DynamicsPoint struct {
	Date  string  `json:"date"`
	Count int64   `json:"count"`
	Share float64 `json:"share"`
}

// Dynamics — результат инструмента dynamics.
type Dynamics struct {
	Phrase   string          `json:"phrase"`
	Period   string          `json:"period"`
	FromDate string          `json:"fromDate"`
	ToDate   string          `json:"toDate"`
	Points   []DynamicsPoint `json:"points"`
	CacheHit bool            `json:"cacheHit"`
	// Regions/Devices — фактически применённый фильтр (эхо сервера).
	Regions []string `json:"regions"`
	Devices []string `json:"devices"`
}

// RegionItem — статистика по одному региону.
// Name заполняется, только если запросили IncludeNames.
type RegionItem struct {
	RegionID      string  `json:"regionId"`
	Name          string  `json:"name"`
	Count         int64   `json:"count"`
	Share         float64 `json:"share"`
	AffinityIndex float64 `json:"affinityIndex"`
}

// Regions — результат инструмента regions (отсортирован по убыванию Count).
type Regions struct {
	Phrase   string       `json:"phrase"`
	Region   string       `json:"region"`
	Items    []RegionItem `json:"regions"`
	CacheHit bool         `json:"cacheHit"`
}

// TopParams — аргументы top_requests.
type TopParams struct {
	Phrase     string
	NumPhrases int
	// Regions — geo ID Яндекса: "213" — Москва, "1" — Москва и область,
	// "225" — Россия. Пусто — вся Россия.
	Regions []string
	// Devices — DEVICE_ALL | DEVICE_DESKTOP | DEVICE_PHONE | DEVICE_TABLET.
	Devices []string
}

// DynamicsParams — аргументы dynamics.
type DynamicsParams struct {
	Phrase   string
	Period   string // daily | weekly | monthly
	FromDate string // RFC3339 или YYYY-MM-DD
	ToDate   string
	Regions  []string
	Devices  []string
}

// RegionsParams — аргументы regions.
type RegionsParams struct {
	Phrase       string
	RegionMode   string // all | cities | regions
	IncludeNames bool
}
