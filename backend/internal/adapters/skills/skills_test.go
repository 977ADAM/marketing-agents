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
