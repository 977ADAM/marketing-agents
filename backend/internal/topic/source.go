package topic

import "context"

// Demand — спрос по фразе: широкая частотность головной фразы и популярные
// формулировки.
//
// TotalCount — единственная корректная мера объёма темы; Requests — подмножества
// TotalCount, поэтому суммировать их нельзя (замерено: превышение в 3.4–5.5 раз).
// Associations — похожие запросы, в подмножество не входят и часто шумят.
type Demand struct {
	Phrase       string
	TotalCount   int64
	Requests     []PhraseCount
	Associations []PhraseCount
	HasData      bool
	CacheHit     bool
}

// DemandParams — что спрашиваем у источника спроса.
type DemandParams struct {
	Phrase     string
	NumPhrases int
	// Regions — geo ID Яндекса: 213 — Москва, 1 — Москва и область, 225 — Россия.
	// Пусто — вся Россия.
	Regions []string
	// Devices — DEVICE_ALL | DEVICE_DESKTOP | DEVICE_PHONE | DEVICE_TABLET.
	Devices []string
}

// DynamicsPoint — точка временного ряда сезонности.
type DynamicsPoint struct {
	Date  string
	Count int64
	Share float64
}

// Dynamics — ряд по фразе за период: по нему считается сезонная поправка.
type Dynamics struct {
	Phrase string
	Period string
	Points []DynamicsPoint
}

// DynamicsParams — аргументы запроса динамики.
type DynamicsParams struct {
	Phrase   string
	Period   string // daily | weekly | monthly
	FromDate string // RFC3339 или YYYY-MM-DD
	ToDate   string
	Regions  []string
	Devices  []string
}

// Source — порт источника спроса: то, что подбору тем нужно от поисковика.
//
// Объявлен здесь, у потребителя: реализации (Wordstat через MCP, Fake в тестах)
// импортируют домен, а домен про них не знает.
type Source interface {
	Demand(ctx context.Context, p DemandParams) (Demand, error)
	Dynamics(ctx context.Context, p DynamicsParams) (Dynamics, error)
}
