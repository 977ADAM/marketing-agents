// Package brief — домен интервью по брифу нативной кампании.
//
// Домен не знает ни про HTTP, ни про SQL, ни про поставщика модели: он описывает
// историю диалога, черновик брифа, итог хода и статусы готовности. Конвертацию
// черновика в campaign.Brief делает транспорт.
package brief

import "strings"

// Message — одна реплика присланной истории.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Draft — черновик брифа, который интервьюер собирает по ходу разговора.
// Он же — форма хвостового JSON ответа модели.
type Draft struct {
	Product  string `json:"product"`
	Goal     string `json:"goal"`
	Audience string `json:"audience"`
	Tone     string `json:"tone"`
	// Region — числовой geo id Яндекса (225 — Россия, 213 — Москва).
	Region string `json:"region,omitempty"`
	// TopicsCount — сколько статей нужно по медиаплану.
	TopicsCount int `json:"topics_count,omitempty"`
}

// HasRequired сообщает, заполнены ли четыре обязательных поля брифа.
// Пробельное значение полем не считается.
func (d Draft) HasRequired() bool {
	return nonEmpty(d.Product) && nonEmpty(d.Goal) && nonEmpty(d.Audience) && nonEmpty(d.Tone)
}

// Статусы готовности брифа. Их считает сервис по обязательным полям, а не модель.
const (
	// StatusReady — обязательных полей хватает для запуска кампании.
	StatusReady = "ready"
	// StatusNeedsInput — пользователь ещё не дал часть обязательных полей.
	StatusNeedsInput = "needs_input"
)

// Result — итог одного хода интервью: реплика ассистента и состояние брифа.
type Result struct {
	Reply string
	Draft Draft
	// Missing — коды незаполненных обязательных полей в порядке product, goal,
	// audience, tone. Пустой список — не nil, чтобы в JSON уходил [].
	Missing []string
	Status  string
}

// nonEmpty — единое правило «поле заполнено» для готовности и слияния.
func nonEmpty(s string) bool { return strings.TrimSpace(s) != "" }
