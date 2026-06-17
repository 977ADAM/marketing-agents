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
}

// Article — выход копирайтера.
type Article struct {
	Topic string `json:"topic"`
	Title string `json:"title"`
	Body  string `json:"body"`
	CTA   string `json:"cta"`
}

// Review — выход критика. Verdict: "accept" | "revise".
type Review struct {
	Score   int      `json:"score"`
	Issues  []string `json:"issues"`
	Verdict string   `json:"verdict"`
}

// Deliverable — статья с прикреплённым ревью (итог по теме).
type Deliverable struct {
	Article
	Review Review `json:"review"`
}
