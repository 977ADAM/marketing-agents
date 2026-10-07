package skills_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/adapters/skills"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
)

type fakeClient struct {
	gotRole, gotSystem, gotUser string
	gotOut                      any
	usage                       corellm.Usage
	err                         error
}

func (f *fakeClient) Complete(_ context.Context, role, system, user string, out any) (corellm.Usage, error) {
	f.gotRole, f.gotSystem, f.gotUser, f.gotOut = role, system, user, out
	return f.usage, f.err
}

const contract = "Ответ строго в JSON: {\"topic\": \"...\"}"

// proseCaveatWant — оговорка прозаической роли. Текст продублирован намеренно:
// тест фиксирует формулировку, а не то, что декоратор сравнивает её с собой.
const proseCaveatWant = `Файлы не создаются: раздел «Результат: …» выше описывает требуемое
содержание, а не файл на диске. Формат ответа задан ниже и важнее инструкций выше.`

// contractCaveatMarker — фраза, которой помечен контракт JSON-роли.
const contractCaveatMarker = "Ответ — только JSON по схеме"

func TestCompleteComposesSkillBeforeContract(t *testing.T) {
	fake := &fakeClient{}
	c, err := skills.New(fake, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Complete(context.Background(), "copywriter", contract, "бриф", &struct{}{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	skill, err := skills.Prompt("native-article")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !strings.HasPrefix(fake.gotSystem, skill) {
		t.Error("промпт не начинается с текста скила")
	}
	if !strings.Contains(fake.gotSystem, "Формат ответа этого сервиса важнее инструкций выше") {
		t.Error("нет оговорки о приоритете формата")
	}
	if !strings.HasSuffix(fake.gotSystem, contract) {
		t.Error("контракт роли должен быть последним")
	}
}

func TestCompletePassesThroughRoleWithoutBinding(t *testing.T) {
	fake := &fakeClient{}
	c, err := skills.New(fake, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Complete(context.Background(), "reviewer", contract, "задача", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if fake.gotSystem != contract {
		t.Errorf("system роли без байнда изменён: %q", fake.gotSystem)
	}
	if fake.gotUser != "задача" {
		t.Errorf("user = %q, want %q", fake.gotUser, "задача")
	}
}

func TestCompleteAddsSkillExactlyOnce(t *testing.T) {
	fake := &fakeClient{}
	c, err := skills.New(fake, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Complete(context.Background(), "copywriter", contract, "бриф", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	skill, err := skills.Prompt("native-article")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got := strings.Count(fake.gotSystem, skill); got != 1 {
		t.Errorf("текст скила встречается %d раз, want 1", got)
	}
}

func TestNewRejectsUnknownSkillName(t *testing.T) {
	_, err := skills.New(&fakeClient{}, skills.Options{Bindings: map[string]string{"copywriter": "нет-такого"}})
	if err == nil {
		t.Fatal("ожидалась ошибка на неизвестный скил")
	}
	if !strings.Contains(err.Error(), "нет-такого") {
		t.Errorf("в ошибке нет имени скила: %v", err)
	}
}

func TestCompletePropagatesUsageErrorAndOut(t *testing.T) {
	sentinel := errors.New("провайдер недоступен")
	wantUsage := corellm.Usage{PromptTokens: 11, CompletionTokens: 22, ReasoningTokens: 3}
	fake := &fakeClient{usage: wantUsage, err: sentinel}
	c, err := skills.New(fake, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := struct{ Topic string }{}
	usage, err := c.Complete(context.Background(), "copywriter", contract, "бриф", &out)
	if !errors.Is(err, sentinel) {
		t.Errorf("ошибка не прокинута: %v", err)
	}
	if !reflect.DeepEqual(usage, wantUsage) {
		t.Errorf("usage = %+v, want %+v", usage, wantUsage)
	}
	if fake.gotOut != any(&out) {
		t.Errorf("out не прокинут: got %p, want %p", fake.gotOut, &out)
	}
	skill, err := skills.Prompt("native-article")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !strings.HasPrefix(fake.gotSystem, skill) {
		t.Error("промпт не составлен при ошибке провайдера")
	}
}

func TestNewRejectsNilInner(t *testing.T) {
	_, err := skills.New(nil, skills.Options{})
	if err == nil {
		t.Fatal("ожидалась ошибка на nil внутренний клиент")
	}
}

// modelFakeClient — внутренний клиент, который умеет называть модель роли.
type modelFakeClient struct {
	fakeClient
	model string
}

func (m *modelFakeClient) ModelFor(string) string { return m.model }

func TestModelForDelegatesToInner(t *testing.T) {
	c, err := skills.New(&modelFakeClient{model: "deepseek-chat"}, skills.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.ModelFor("copywriter"); got != "deepseek-chat" {
		t.Errorf("ModelFor = %q, want %q", got, "deepseek-chat")
	}
}

func TestModelForEmptyWhenInnerCannotNameModel(t *testing.T) {
	c, err := skills.New(&fakeClient{}, skills.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.ModelFor("copywriter"); got != "" {
		t.Errorf("ModelFor = %q, want пустую строку", got)
	}
}

// streamFakeClient — внутренний клиент, который умеет стримить: запоминает
// промпты и отдаёт наружу заранее заданные фрагменты.
type streamFakeClient struct {
	fakeClient
	deltas []string
}

func (f *streamFakeClient) CompleteStream(_ context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error) {
	f.gotRole, f.gotSystem, f.gotUser = role, system, user
	for _, delta := range f.deltas {
		onDelta(delta)
	}
	return f.usage, f.err
}

// Декоратор обязан пробрасывать стрим: иначе утверждение типа на corellm.Streamer
// в composition root не соберётся.
var _ corellm.Streamer = (*skills.Client)(nil)

func TestCompleteStreamProseRoleGetsProseCaveat(t *testing.T) {
	fake := &streamFakeClient{}
	c, err := skills.New(fake, skills.Options{
		Bindings: map[string]string{"interviewer": "native-article"},
		Prose:    map[string]bool{"interviewer": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.CompleteStream(context.Background(), "interviewer", contract, "бриф", func(string) {}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	skill, err := skills.Prompt("native-article")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !strings.HasPrefix(fake.gotSystem, skill) {
		t.Error("промпт не начинается с текста скила")
	}
	if !strings.Contains(fake.gotSystem, proseCaveatWant) {
		t.Errorf("нет прозаической оговорки: %q", fake.gotSystem)
	}
	if strings.Contains(fake.gotSystem, contractCaveatMarker) {
		t.Error("прозаической роли подставлен контракт JSON")
	}
	if !strings.HasSuffix(fake.gotSystem, contract) {
		t.Error("контракт роли должен быть последним")
	}
}

func TestCompleteStreamNonProseRoleKeepsContractCaveat(t *testing.T) {
	fake := &streamFakeClient{}
	c, err := skills.New(fake, skills.Options{
		Bindings: map[string]string{"copywriter": "native-article"},
		Prose:    map[string]bool{"interviewer": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.CompleteStream(context.Background(), "copywriter", contract, "бриф", func(string) {}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if !strings.Contains(fake.gotSystem, contractCaveatMarker) {
		t.Errorf("обычная роль потеряла контракт JSON: %q", fake.gotSystem)
	}
	if strings.Contains(fake.gotSystem, proseCaveatWant) {
		t.Error("обычной роли подставлена прозаическая оговорка")
	}
}

// Прозаическая роль без байнда скила — как и в Complete, промпт не трогаем.
func TestCompleteStreamRoleWithoutBindingIsUntouched(t *testing.T) {
	fake := &streamFakeClient{}
	c, err := skills.New(fake, skills.Options{
		Bindings: map[string]string{"copywriter": "native-article"},
		Prose:    map[string]bool{"interviewer": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.CompleteStream(context.Background(), "interviewer", contract, "бриф", func(string) {}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if fake.gotSystem != contract {
		t.Errorf("system роли без байнда изменён: %q", fake.gotSystem)
	}
}

// Стрим собирает system теми же тремя частями, что и Complete: текст скила,
// оговорка, контракт роли.
func TestCompleteStreamComposesSystemLikeComplete(t *testing.T) {
	opts := skills.Options{Bindings: map[string]string{"copywriter": "native-article"}}

	syncFake := &fakeClient{}
	syncClient, err := skills.New(syncFake, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := syncClient.Complete(context.Background(), "copywriter", contract, "бриф", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	streamFake := &streamFakeClient{}
	streamClient, err := skills.New(streamFake, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := streamClient.CompleteStream(context.Background(), "copywriter", contract, "бриф", func(string) {}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}

	if streamFake.gotSystem != syncFake.gotSystem {
		t.Errorf("system стрима = %q, want %q", streamFake.gotSystem, syncFake.gotSystem)
	}
}

func TestCompleteStreamForwardsDeltasUsageAndError(t *testing.T) {
	wantUsage := corellm.Usage{PromptTokens: 5, CompletionTokens: 6, Response: "привет"}
	sentinel := errors.New("поток прерван")
	fake := &streamFakeClient{deltas: []string{"при", "вет"}}
	fake.usage, fake.err = wantUsage, sentinel
	c, err := skills.New(fake, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var got []string
	usage, err := c.CompleteStream(context.Background(), "copywriter", contract, "бриф", func(delta string) {
		got = append(got, delta)
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("ошибка не прокинута: %v", err)
	}
	if !reflect.DeepEqual(usage, wantUsage) {
		t.Errorf("usage = %+v, want %+v", usage, wantUsage)
	}
	if !reflect.DeepEqual(got, []string{"при", "вет"}) {
		t.Errorf("onDelta получил %q, want %q", got, []string{"при", "вет"})
	}
	if fake.gotUser != "бриф" {
		t.Errorf("user = %q, want %q", fake.gotUser, "бриф")
	}
}

func TestCompleteStreamWithoutStreamingInnerReturnsError(t *testing.T) {
	c, err := skills.New(&fakeClient{}, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.CompleteStream(context.Background(), "copywriter", contract, "бриф", func(string) {})
	if err == nil {
		t.Fatal("ожидалась ошибка на нестриминговый внутренний клиент")
	}
	if !strings.Contains(err.Error(), "skills: клиент не поддерживает стриминг") {
		t.Errorf("err = %v", err)
	}
}
