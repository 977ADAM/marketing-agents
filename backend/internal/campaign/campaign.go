// Package campaign — домен генерации кампании: бриф, стратегия, статьи и их
// ревью. Домен: не знает ни про HTTP, ни про SQL, ни про поставщиков спроса.
package campaign

import (
	"github.com/977ADAM/marketing-agents/internal/topic"
)

// Brief — вход пайплайна.
type Brief struct {
	Product  string `json:"product"`
	Goal     string `json:"goal"`
	Audience string `json:"audience"`
	Tone     string `json:"tone"`
	// Region — geo ID Яндекса для подбора тем: 225 — Россия, 1 — Москва и
	// область, 213 — Москва. Пусто — регион по умолчанию из конфига.
	Region string `json:"region,omitempty"`
	// TopicsCount — сколько статей нужно по медиаплану. Идей предлагаем вдвое
	// больше (см. TOPICS_MULTIPLIER), в генерацию уходят лучшие.
	TopicsCount int `json:"topics_count,omitempty"`
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
	TopicCandidates []topic.TopicCandidate `json:"topic_candidates,omitempty"`
	// WordstatCalls — сколько обращений к Wordstat потребовал подбор тем.
	WordstatCalls int `json:"wordstat_calls,omitempty"`
}

// Article — выход копирайтера.
type Article struct {
	Topic string `json:"topic"`
	Title string `json:"title"`
	Body  string `json:"body"`
	CTA   string `json:"cta"`
}

// Review — выход критика. Verdict: "accept" | "revise".
// Severity — градация score для интерфейса (см. score.Severity).
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

// Briefing — проекция брифа для подбора тем: topic не знает про Brief (иначе
// получился бы цикл campaign ↔ topic), поэтому подбор принимает свой вход.
func (b Brief) Briefing() topic.Briefing {
	return topic.Briefing{Product: b.Product, Goal: b.Goal, Audience: b.Audience, Tone: b.Tone}
}
