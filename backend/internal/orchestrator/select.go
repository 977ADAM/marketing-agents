package orchestrator

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
)

// SelectOptions — правила отбора тем из настроек (config).
type SelectOptions struct {
	// MinVolume — минимальный объём темы, показов за 30 дней.
	MinVolume int64
	// SeasonalityFactor — во сколько раз пик за 12 месяцев должен превышать
	// порог, чтобы сезонную тему не отсеяли вне сезона.
	SeasonalityFactor float64
}

// DraftInput — тема-кандидат от модели вместе с собранными по ней данными.
// Source: wordstat (есть цитаты и частотности) или llm (тема «от себя», без цифр).
type DraftInput struct {
	Draft  agents.TopicDraft
	Source string
	// Queries — цитаты с частотностями из ответов Wordstat.
	Queries []agents.PhraseCount
	// Season — сезонная поправка головной фразы, если dynamics запрашивали.
	Season *agents.Seasonality
}

// Отклонённые темы помечаем причиной — так в результате видно, почему тема не
// пошла в генерацию, а не просто «её нет».
const (
	rejectTechnical = "технический запрос (размер или модель)"
	rejectLowVolume = "объём ниже порога"
)

// Интенты запросов. Градация примитивная и намеренно прозрачная: она нужна,
// чтобы отличить вопрос (материал для темы) от размера или коммерции.
var intentPatterns = []struct {
	intent string
	re     *regexp.Regexp
}{
	{"вопрос", regexp.MustCompile(`(^|\s)(как|какую|какая|какой|какие|чем|почему|сколько|можно|нужно|стоит|или)(\s|$)`)},
	{"сравнение", regexp.MustCompile(`(^|\s)(лучше|лучшая|лучший|лучшие|рейтинг|сравнение)(\s|$)`)},
	{"выбор", regexp.MustCompile(`(^|\s)(выбрать|подобрать|подбор|выбор)(\s|$)`)},
	{"коммерческий", regexp.MustCompile(`(^|\s)(купить|куплю|цена|цены|стоимость|заказать|авито|бу|дешево|дешевле)(\s|$)`)},
}

// technicalRE ловит размеры, типоразмеры и модели — в «зимняя резина 205 55 16»
// спрос большой, но это не тема статьи.
var technicalRE = regexp.MustCompile(`(^|\s)(\d{2}|\d{2}\s*\d{2}|r\d{2})(\s|$)`)

// IntentOf классифицирует запрос по интенту (или "" для нейтрального).
func IntentOf(phrase string) string {
	p := strings.ToLower(strings.TrimSpace(phrase))
	if p == "" {
		return ""
	}
	for _, ip := range intentPatterns {
		if ip.re.MatchString(p) {
			return ip.intent
		}
	}
	return ""
}

// isTechnical сообщает, что фраза описывает размер или модель, а не тему.
func isTechnical(phrase string) bool {
	return technicalRE.MatchString(strings.ToLower(strings.TrimSpace(phrase)))
}

// SeasonalityOf считает сезонную поправку по ряду dynamics: пик, дно, размах.
// Seasonal = размах не меньше factor — тогда тему не отсекаем по «летнему» спросу.
func SeasonalityOf(points []wordstat.DynamicsPoint, factor float64) *agents.Seasonality {
	if len(points) == 0 {
		return nil
	}
	peak, trough := points[0], points[0]
	for _, p := range points {
		if p.Count > peak.Count {
			peak = p
		}
		if p.Count < trough.Count {
			trough = p
		}
	}
	s := &agents.Seasonality{Peak: peak.Count, PeakMonth: monthOf(peak.Date), Trough: trough.Count}
	if trough.Count > 0 {
		s.Ratio = float64(peak.Count) / float64(trough.Count)
	}
	if factor < 1 {
		factor = 1
	}
	s.Seasonal = s.Ratio >= factor
	return s
}

// monthOf приводит дату точки к «YYYY-MM» (дата приходит в RFC3339).
func monthOf(date string) string {
	if t, err := time.Parse(time.RFC3339, date); err == nil {
		return t.Format("2006-01")
	}
	return date
}

// SelectTopics собирает кандидатов из драфтов, отбирает want лучших и помечает
// остальных причиной отклонения.
//
// Объём темы — максимум по её цитатам, а не сумма: популярные запросы являются
// подмножествами широкой частотности, и суммирование завышает спрос в разы
// (замерено 3.4–5.5x на живых данных).
//
// Порядок результата: отобранные сверху, далее по убыванию объёма.
func SelectTopics(inputs []DraftInput, want int, opt SelectOptions) []agents.TopicCandidate {
	if want < 0 {
		want = 0
	}

	cands := make([]agents.TopicCandidate, 0, len(inputs))
	for i, in := range inputs {
		source := in.Source
		if source == "" {
			source = agents.SourceWordstat
		}
		c := agents.TopicCandidate{
			ID:      fmt.Sprintf("t%d", i+1),
			Title:   in.Draft.Title,
			Goal:    in.Draft.Goal,
			Task:    in.Draft.Task,
			Source:  source,
			Queries: in.Queries,
			Season:  in.Season,
		}

		// Головная фраза — самая частотная из цитат; она же задаёт объём.
		for _, q := range in.Queries {
			if q.Count > c.Volume {
				c.Volume, c.Head = q.Count, q.Phrase
			}
		}
		c.Intent = in.Draft.Intent
		if c.Intent == "" {
			c.Intent = IntentOf(c.Head)
		}

		// Темы без данных (llm) порогом не судим: у них нет цифр по определению.
		if source == agents.SourceWordstat {
			switch {
			case isTechnical(c.Head):
				c.Reject = rejectTechnical
			case c.Volume < opt.MinVolume && !seasonalRescue(c, opt):
				c.Reject = rejectLowVolume
			}
		}
		cands = append(cands, c)
	}

	// Ранжирование: у сезонной темы в межсезонье сравниваем по пику, иначе
	// зимняя тема в июне ушла бы в конец списка.
	sort.SliceStable(cands, func(i, j int) bool {
		vi, vj := rankVolume(cands[i]), rankVolume(cands[j])
		if vi != vj {
			return vi > vj
		}
		return cands[i].Title < cands[j].Title
	})

	selected := 0
	for i := range cands {
		if selected >= want || cands[i].Reject != "" {
			continue
		}
		cands[i].Selected = true
		selected++
	}
	return cands
}

// seasonalRescue пропускает тему ниже порога, если её пик за 12 месяцев
// дотягивает до порога, а размах достаточно велик.
func seasonalRescue(c agents.TopicCandidate, opt SelectOptions) bool {
	if c.Season == nil || !c.Season.Seasonal {
		return false
	}
	return c.Season.Peak >= opt.MinVolume
}

// rankVolume — объём для сортировки: фактический, а для сезонных тем — не меньше
// сезонного пика.
func rankVolume(c agents.TopicCandidate) int64 {
	if c.Season != nil && c.Season.Seasonal && c.Season.Peak > c.Volume {
		return c.Season.Peak
	}
	return c.Volume
}

// SelectedTopics превращает отобранные кандидаты в темы для копирайтеров.
// Ритм запросов становится тезисами статьи: копирайтер пишет по реальному спросу.
func SelectedTopics(cands []agents.TopicCandidate) []agents.Topic {
	out := make([]agents.Topic, 0, len(cands))
	for _, c := range cands {
		if c.Selected {
			out = append(out, TopicFromCandidate(c))
		}
	}
	return out
}

// TopicFromCandidate отображает кандидата в тему пайплайна.
func TopicFromCandidate(c agents.TopicCandidate) agents.Topic {
	points := make([]string, 0, len(c.Queries))
	for _, q := range c.Queries {
		points = append(points, q.Phrase)
	}
	return agents.Topic{Title: c.Title, Angle: c.Goal, Points: points}
}

// FallbackInputs превращает темы «от себя» в кандидатов без цифр: они попадают
// в конец списка и добираются только если подтверждённых спросом не хватило.
func FallbackInputs(drafts []agents.TopicDraft) []DraftInput {
	out := make([]DraftInput, 0, len(drafts))
	for _, d := range drafts {
		out = append(out, DraftInput{Draft: d, Source: agents.SourceLLM})
	}
	return out
}
