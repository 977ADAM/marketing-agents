package briefservice_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/core/limits"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
	briefservice "github.com/977ADAM/marketing-agents/internal/features/brief/service"
)

// fakeStreamer отдаёт заранее заданный ответ фрагментами и запоминает запрос.
type fakeStreamer struct {
	chunks []string
	usage  corellm.Usage
	err    error

	role, system, user string
	calls              int
}

func (f *fakeStreamer) CompleteStream(_ context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error) {
	f.role, f.system, f.user = role, system, user
	f.calls++
	for _, c := range f.chunks {
		if onDelta != nil {
			onDelta(c)
		}
	}
	return f.usage, f.err
}

// history собирает историю диалога: чётные сообщения — пользователь,
// нечётные — ассистент.
func history(texts ...string) []brief.Message {
	msgs := make([]brief.Message, 0, len(texts))
	for i, text := range texts {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, brief.Message{Role: role, Content: text})
	}
	return msgs
}

const allFieldsTail = "{\"product\":\"Кружка\",\"goal\":\"Рост\",\"audience\":\"ЗОЖ\",\"tone\":\"дружелюбный\"}"

func TestAskSendsProseToClientAndParsesTail(t *testing.T) {
	fake := &fakeStreamer{
		chunks: []string{
			"Здравствуйте! ",
			"Расскажите о продукте.\n<<<BR",
			"IEF\n{\"product\":\"Кружка\"}",
		},
		usage: corellm.Usage{PromptTokens: 5, CompletionTokens: 7},
	}
	s := briefservice.New(briefservice.Options{Stream: fake})

	var sent strings.Builder
	res, usage, err := s.Ask(context.Background(), history("Хочу кампанию"), brief.Draft{}, func(d string) {
		sent.WriteString(d)
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if usage.PromptTokens != 5 || usage.CompletionTokens != 7 {
		t.Errorf("usage = %+v, want 5/7", usage)
	}

	want := "Здравствуйте! Расскажите о продукте.\n"
	if got := sent.String(); got != want {
		t.Errorf("наружу ушло %q, want %q", got, want)
	}
	if strings.Contains(sent.String(), "<<<BRIEF") || strings.Contains(sent.String(), "Кружка") {
		t.Errorf("хвост утёк в поток: %q", sent.String())
	}
	if res.Reply != strings.TrimSpace(want) {
		t.Errorf("Reply = %q, want %q", res.Reply, strings.TrimSpace(want))
	}
	if res.Draft.Product != "Кружка" {
		t.Errorf("Draft.Product = %q, want Кружка", res.Draft.Product)
	}
	if res.Draft.Goal != "" || res.Draft.Audience != "" || res.Draft.Tone != "" {
		t.Errorf("в бриф попали чужие поля: %+v", res.Draft)
	}
	if fake.role != briefservice.RoleInterviewer {
		t.Errorf("role = %q, want %q", fake.role, briefservice.RoleInterviewer)
	}
}

func TestAskComputesMissingAndStatus(t *testing.T) {
	t.Run("двух обязательных полей не хватает", func(t *testing.T) {
		fake := &fakeStreamer{chunks: []string{"Понял.\n<<<BRIEF\n{\"product\":\"Кружка\",\"tone\":\"дружелюбный\"}"}}
		s := briefservice.New(briefservice.Options{Stream: fake})

		res, _, err := s.Ask(context.Background(), history("Хочу кампанию"), brief.Draft{}, nil)
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		want := []string{"goal", "audience"}
		if len(res.Missing) != len(want) || res.Missing[0] != want[0] || res.Missing[1] != want[1] {
			t.Errorf("Missing = %v, want %v", res.Missing, want)
		}
		if res.Status != brief.StatusNeedsInput {
			t.Errorf("Status = %q, want %q", res.Status, brief.StatusNeedsInput)
		}
	})

	t.Run("все обязательные поля собраны", func(t *testing.T) {
		fake := &fakeStreamer{chunks: []string{"Готово.\n<<<BRIEF\n" + allFieldsTail}}
		s := briefservice.New(briefservice.Options{Stream: fake})

		res, _, err := s.Ask(context.Background(), history("Хочу кампанию"), brief.Draft{}, nil)
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if len(res.Missing) != 0 {
			t.Errorf("Missing = %v, want пусто", res.Missing)
		}
		if res.Missing == nil {
			t.Error("Missing = nil: в JSON кадра уйдёт null вместо []")
		}
		if res.Status != brief.StatusReady {
			t.Errorf("Status = %q, want %q", res.Status, brief.StatusReady)
		}
		if !res.Draft.HasRequired() {
			t.Errorf("бриф не признан полным: %+v", res.Draft)
		}
	})

	t.Run("прежний бриф учитывается при неполном хвосте", func(t *testing.T) {
		fake := &fakeStreamer{chunks: []string{"Дополнил.\n<<<BRIEF\n{\"tone\":\"строгий\"}"}}
		s := briefservice.New(briefservice.Options{Stream: fake})
		prev := brief.Draft{Product: "Кружка", Goal: "Рост", Audience: "ЗОЖ"}

		res, _, err := s.Ask(context.Background(), history("Хочу кампанию"), prev, nil)
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if res.Status != brief.StatusReady || len(res.Missing) != 0 {
			t.Errorf("статус = %q, Missing = %v; want ready и пусто", res.Status, res.Missing)
		}
		if res.Draft.Tone != "строгий" || res.Draft.Product != "Кружка" {
			t.Errorf("слияние с прежним брифом сломано: %+v", res.Draft)
		}
	})
}

func TestAskKeepsPreviousDraftOnBrokenTail(t *testing.T) {
	prev := brief.Draft{
		Product: "Кружка", Goal: "Рост продаж", Audience: "ЗОЖ 25–40",
		Tone: "строгий", Region: "225", TopicsCount: 5,
	}
	fake := &fakeStreamer{chunks: []string{"Понял вас, сейчас поправлю.\n<<<BRIEF\n{битый"}}
	s := briefservice.New(briefservice.Options{Stream: fake})

	res, _, err := s.Ask(context.Background(), history("Измени тон"), prev, nil)
	if err != nil {
		t.Fatalf("битый хвост не должен ронять ход: %v", err)
	}
	if res.Draft != prev {
		t.Errorf("Draft = %+v, want прежний %+v", res.Draft, prev)
	}
	if !strings.Contains(res.Reply, "Понял вас") {
		t.Errorf("Reply потерял прозу: %q", res.Reply)
	}
	if strings.Contains(res.Reply, "<<<BRIEF") {
		t.Errorf("маркер утёк в Reply: %q", res.Reply)
	}
	if res.Status != brief.StatusReady || len(res.Missing) != 0 {
		t.Errorf("статус прежнего брифа не сохранён: %q, %v", res.Status, res.Missing)
	}
}

func TestAskPromptCarriesTurnLimit(t *testing.T) {
	fake := &fakeStreamer{chunks: []string{"Привет"}}
	s := briefservice.New(briefservice.Options{Stream: fake})

	if _, _, err := s.Ask(context.Background(), history("Мне нужна кампания", "Здравствуйте"), brief.Draft{}, nil); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if fake.role != briefservice.RoleInterviewer {
		t.Errorf("role = %q, want %q", fake.role, briefservice.RoleInterviewer)
	}
	for _, want := range []string{"трёх уточняющих вопросов", "<<<BRIEF"} {
		if !strings.Contains(fake.system, want) {
			t.Errorf("в system нет %q:\n%s", want, fake.system)
		}
	}
	for _, want := range []string{"Пользователь: Мне нужна кампания", "Ассистент: Здравствуйте"} {
		if !strings.Contains(fake.user, want) {
			t.Errorf("в user нет %q:\n%s", want, fake.user)
		}
	}
}

func TestAskPropagatesStreamError(t *testing.T) {
	sentinel := errors.New("провайдер недоступен")
	prev := brief.Draft{Product: "Кружка"}
	fake := &fakeStreamer{
		chunks: []string{"Понял вас"},
		err:    sentinel,
		usage:  corellm.Usage{PromptTokens: 11, CompletionTokens: 4},
	}
	s := briefservice.New(briefservice.Options{Stream: fake})

	res, usage, err := s.Ask(context.Background(), history("Хочу кампанию"), prev, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if res.Reply != "Понял вас" {
		t.Errorf("Reply = %q, want накопленную прозу", res.Reply)
	}
	if res.Draft != prev {
		t.Errorf("Draft = %+v, want прежний %+v", res.Draft, prev)
	}
	if usage.PromptTokens != 11 || usage.CompletionTokens != 4 {
		t.Errorf("usage = %+v, want 11/4", usage)
	}
}

func TestAskLogsDurationAndTokens(t *testing.T) {
	fake := &fakeStreamer{
		chunks: []string{"Готово.\n<<<BRIEF\n" + allFieldsTail},
		usage:  corellm.Usage{PromptTokens: 120, CompletionTokens: 45},
	}
	log := &fakeLogger{}
	s := briefservice.New(briefservice.Options{Stream: fake, Log: log})

	msgs := history("Хочу кампанию")
	res, _, err := s.Ask(context.Background(), msgs, brief.Draft{}, nil)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(log.calls) != 1 {
		t.Fatalf("строк в журнале: %d, want 1: %+v", len(log.calls), log.calls)
	}
	call := log.calls[0]
	if call.msg != "interview" {
		t.Errorf("сообщение журнала = %q, want interview", call.msg)
	}
	if got := logValue(t, call.args, "prompt_tokens"); got != 120 {
		t.Errorf("prompt_tokens = %v, want 120", got)
	}
	if got := logValue(t, call.args, "completion_tokens"); got != 45 {
		t.Errorf("completion_tokens = %v, want 45", got)
	}
	if got := logValue(t, call.args, "messages"); got != len(msgs) {
		t.Errorf("messages = %v, want %d", got, len(msgs))
	}
	if got := logValue(t, call.args, "status"); got != res.Status {
		t.Errorf("status = %v, want %q", got, res.Status)
	}
	if got, ok := logValue(t, call.args, "duration_ms").(int64); !ok || got < 0 {
		t.Errorf("duration_ms = %v, want неотрицательное число миллисекунд", logValue(t, call.args, "duration_ms"))
	}

	// Журнал не обязателен: nil подменяется заглушкой и не паникует.
	quiet := briefservice.New(briefservice.Options{Stream: &fakeStreamer{chunks: []string{"ок"}}})
	if res, _, err := quiet.Ask(context.Background(), msgs, brief.Draft{}, nil); err != nil || res.Reply != "ок" {
		t.Fatalf("Ask без журнала: res=%+v, err=%v", res, err)
	}
}

func TestAskRejectsEmptyHistory(t *testing.T) {
	fake := &fakeStreamer{}
	s := briefservice.New(briefservice.Options{Stream: fake})

	res, _, err := s.Ask(context.Background(), nil, brief.Draft{}, func(string) {})
	assertValidationCode(t, err, "empty_history")
	if fake.calls != 0 {
		t.Errorf("модель вызвана %d раз на пустой истории", fake.calls)
	}
	if res.Status != "" || res.Reply != "" || res.Draft != (brief.Draft{}) || res.Missing != nil {
		t.Errorf("Result не нулевой на ошибке валидации: %+v", res)
	}
}

func TestAskRejectsTooLongHistory(t *testing.T) {
	s := briefservice.New(briefservice.Options{Stream: &fakeStreamer{}})

	msgs := make([]brief.Message, briefservice.MaxMessages+1)
	for i := range msgs {
		msgs[i] = brief.Message{Role: "user", Content: "привет"}
	}
	_, _, err := s.Ask(context.Background(), msgs, brief.Draft{}, nil)
	assertValidationCode(t, err, "history_too_long")

	big := []brief.Message{{Role: "user", Content: strings.Repeat("a", briefservice.MaxChars+1)}}
	_, _, err = s.Ask(context.Background(), big, brief.Draft{}, nil)
	assertValidationCode(t, err, "history_too_long")
}

func TestAskAcceptsHistoryAtLimit(t *testing.T) {
	fake := &fakeStreamer{}
	s := briefservice.New(briefservice.Options{Stream: fake})

	msgs := make([]brief.Message, briefservice.MaxMessages)
	for i := range msgs {
		msgs[i] = brief.Message{Role: "user", Content: "ок"}
	}
	fake.chunks = []string{strings.Repeat("a", briefservice.MaxChars)}
	if _, _, err := s.Ask(context.Background(), msgs, brief.Draft{}, nil); err != nil {
		t.Fatalf("история ровно на границе лимита отвергнута: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("calls = %d, want 1", fake.calls)
	}
}

func TestAskLimitsComeFromOptions(t *testing.T) {
	fake := &fakeStreamer{}
	// «раз» + «два» — 12 байт: ровно на границе настроенного лимита.
	s := briefservice.New(briefservice.Options{Stream: fake, MaxMessages: 2, MaxChars: 12})

	_, _, err := s.Ask(context.Background(), history("раз", "два", "три"), brief.Draft{}, nil)
	assertValidationCode(t, err, "history_too_long")

	fake.chunks = []string{"ок"}
	if _, _, err := s.Ask(context.Background(), history("раз", "два"), brief.Draft{}, nil); err != nil {
		t.Fatalf("история в границах настроенного лимита отвергнута: %v", err)
	}

	_, _, err = s.Ask(context.Background(), history(strings.Repeat("a", 13)), brief.Draft{}, nil)
	assertValidationCode(t, err, "history_too_long")
}

// fakeLogger запоминает вызовы Info, чтобы проверить единственную строку на ход.
type fakeLogger struct {
	calls []logCall
}

type logCall struct {
	msg  string
	args []any
}

func (f *fakeLogger) Debug(string, ...any) {}
func (f *fakeLogger) Info(msg string, args ...any) {
	f.calls = append(f.calls, logCall{msg: msg, args: args})
}
func (f *fakeLogger) Warn(string, ...any)  {}
func (f *fakeLogger) Error(string, ...any) {}

// logValue достаёт значение из пар «ключ-значение» журнального вызова.
func logValue(t *testing.T, args []any, key string) any {
	t.Helper()
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return args[i+1]
		}
	}
	t.Fatalf("в журнале нет поля %q: %v", key, args)
	return nil
}

// assertValidationCode проверяет, что ошибка — валидационная и несёт нужный код.
func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	var validation *limits.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("ожидалась ошибка валидации %q, получено %v", code, err)
	}
	if got := validation.Error(); got != code {
		t.Errorf("код ошибки = %q, want %q", got, code)
	}
}
