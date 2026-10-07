package accounting_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/adapters/accounting"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
)

// fakeStreamClient — внутренний клиент, умеющий и обычный вызов, и стрим.
type fakeStreamClient struct {
	usage              corellm.Usage
	err                error
	model              string
	deltas             []string
	gotRole            string
	gotSystem, gotUser string
}

func (f *fakeStreamClient) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	return f.usage, f.err
}

func (f *fakeStreamClient) CompleteStream(_ context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error) {
	f.gotRole, f.gotSystem, f.gotUser = role, system, user
	for _, delta := range f.deltas {
		onDelta(delta)
	}
	return f.usage, f.err
}

func (f *fakeStreamClient) ModelFor(string) string { return f.model }

// sinkCapture собирает записи расхода, которые декоратор отдаёт в контекст.
type sinkCapture struct {
	entries []corellm.UsageEntry
	err     error
}

func (s *sinkCapture) collect(_ context.Context, e corellm.UsageEntry) error {
	s.entries = append(s.entries, e)
	return s.err
}

// ctxWithSink — контекст с приёмником расхода: так декоратор узнаёт, куда писать.
func ctxWithSink(s *sinkCapture) context.Context {
	return corellm.WithUsageSink(context.Background(), s.collect)
}

// noStreamClient — клиент без стрима: декоратор обязан вернуть понятную ошибку.
type noStreamClient struct{}

func (noStreamClient) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	return corellm.Usage{}, nil
}

// Декоратор обязан пробрасывать стрим: иначе утверждение типа на corellm.Streamer
// в composition root не соберётся.
var _ corellm.Streamer = (*accounting.Client)(nil)

// Записи из Usage.Entries стримингового вызова сохраняются: декоратор дописывает
// роль и идентификатор, как и в обычном вызове.
func TestCompleteStreamWritesUsageEntries(t *testing.T) {
	sink := &sinkCapture{}
	inner := &fakeStreamClient{usage: corellm.Usage{
		PromptTokens: 11, CompletionTokens: 22,
		Entries: []corellm.UsageEntry{{Model: "deepseek-chat", PromptTokens: 11, CompletionTokens: 22}},
	}}
	client := accounting.New(inner)

	usage, err := client.CompleteStream(ctxWithSink(sink), "interviewer", "s", "u", func(string) {})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("записей %d, want 1", len(sink.entries))
	}
	got := sink.entries[0]
	if got.Model != "deepseek-chat" || got.Role != "interviewer" {
		t.Errorf("entry = %+v", got)
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 22 {
		t.Errorf("токены потерялись: %+v", got)
	}
	if got.ID == "" {
		t.Error("идентификатор записи не проставлен")
	}
	if len(usage.Entries) != 1 || usage.Entries[0].Role != "interviewer" {
		t.Errorf("usage.Entries = %+v", usage.Entries)
	}
}

// Пустые Entries при известной роли: синтетическая запись берёт модель из ModelFor.
func TestCompleteStreamFallsBackToModelFor(t *testing.T) {
	sink := &sinkCapture{}
	inner := &fakeStreamClient{
		usage: corellm.Usage{PromptTokens: 7, CompletionTokens: 8},
		model: "deepseek-v4-flash",
	}
	client := accounting.New(inner)

	if _, err := client.CompleteStream(ctxWithSink(sink), "interviewer", "s", "u", func(string) {}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("записей %d, want 1", len(sink.entries))
	}
	got := sink.entries[0]
	if got.Model != "deepseek-v4-flash" || got.Role != "interviewer" {
		t.Errorf("entry = %+v", got)
	}
	if got.PromptTokens != 7 || got.CompletionTokens != 8 {
		t.Errorf("токены потерялись: %+v", got)
	}
}

func TestCompleteStreamPropagatesErrorAndDeltas(t *testing.T) {
	sink := &sinkCapture{}
	sentinel := errors.New("поток прерван")
	inner := &fakeStreamClient{
		usage:  corellm.Usage{Response: "начало"},
		err:    sentinel,
		deltas: []string{"на", "чало"},
	}
	client := accounting.New(inner)

	var got []string
	usage, err := client.CompleteStream(ctxWithSink(sink), "interviewer", "s", "u", func(delta string) {
		got = append(got, delta)
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("ошибка не прокинута: %v", err)
	}
	if usage.Response != "начало" {
		t.Errorf("usage = %+v", usage)
	}
	if strings.Join(got, "") != "начало" {
		t.Errorf("onDelta = %q", got)
	}
	// Расход считается и у прерванного потока: провайдер уже потратил токены.
	if len(sink.entries) != 1 {
		t.Errorf("записей %d, want 1", len(sink.entries))
	}
}

func TestCompleteStreamWrapsSinkError(t *testing.T) {
	sink := &sinkCapture{err: errors.New("БД недоступна")}
	inner := &fakeStreamClient{usage: corellm.Usage{PromptTokens: 1, CompletionTokens: 2}}
	client := accounting.New(inner)

	_, err := client.CompleteStream(ctxWithSink(sink), "interviewer", "s", "u", func(string) {})
	if err == nil {
		t.Fatal("ожидалась ошибка записи расхода")
	}
	if !strings.Contains(err.Error(), "persist usage") || !strings.Contains(err.Error(), "БД недоступна") {
		t.Errorf("err = %v", err)
	}
}

func TestCompleteStreamWithoutStreamingInnerReturnsError(t *testing.T) {
	client := accounting.New(noStreamClient{})
	_, err := client.CompleteStream(context.Background(), "interviewer", "s", "u", func(string) {})
	if err == nil {
		t.Fatal("ожидалась ошибка на нестриминговый внутренний клиент")
	}
	if !strings.Contains(err.Error(), "accounting: клиент не поддерживает стриминг") {
		t.Errorf("err = %v", err)
	}
}
