package briefservice

import (
	"encoding/json"
	"strings"

	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
)

// tailMarker — начало машинного блока с брифом в конце ответа модели.
const tailMarker = "<<<BRIEF"

// ParseTail достаёт бриф из машинного хвоста ответа модели.
//
// Разбор намеренно терпимый: берём последнее вхождение маркера (в прозе он может
// упоминаться и раньше), снимаем код-фенс и разбираем JSON. Любая неудача — это
// false без ошибки: реплика модели всё равно доходит до пользователя, а бриф
// остаётся прежним, поэтому чат не рвётся.
func ParseTail(text string) (brief.Draft, bool) {
	i := strings.LastIndex(text, tailMarker)
	if i < 0 {
		return brief.Draft{}, false
	}
	var draft brief.Draft
	if err := json.Unmarshal([]byte(stripFence(text[i+len(tailMarker):])), &draft); err != nil {
		return brief.Draft{}, false
	}
	return draft, true
}

// stripFence снимает обрамление ```json … ```, если модель его добавила.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		s = strings.TrimPrefix(s, "```")
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// mergeDraft накладывает разобранный хвост на прежний бриф: непустые поля хвоста
// перекрывают прежние значения, пустые — не затирают уже собранное, поэтому
// частичный ответ модели безопасен.
func mergeDraft(prev, tail brief.Draft) brief.Draft {
	if filled(tail.Product) {
		prev.Product = tail.Product
	}
	if filled(tail.Goal) {
		prev.Goal = tail.Goal
	}
	if filled(tail.Audience) {
		prev.Audience = tail.Audience
	}
	if filled(tail.Tone) {
		prev.Tone = tail.Tone
	}
	if filled(tail.Region) {
		prev.Region = tail.Region
	}
	if tail.TopicsCount > 0 {
		prev.TopicsCount = tail.TopicsCount
	}
	return prev
}

// filled — «поле непусто»: пробельное значение не считается заполненным.
func filled(s string) bool { return strings.TrimSpace(s) != "" }
