package main

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/adapters/skills"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	briefservice "github.com/977ADAM/marketing-agents/internal/features/brief/service"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

func TestSkillBindingsCoverAllRoles(t *testing.T) {
	want := map[string]string{
		campaignservice.RoleStrategist: "campaign-plan",
		campaignservice.RoleCopywriter: "native-article",
		campaignservice.RoleCritic:     "article-review",
		reviewservice.RoleCompliance:   "article-review",
		reviewservice.RoleQuality:      "article-review",
		topicservice.RoleSeeds:         "campaign-plan",
		topicservice.RoleCluster:       "campaign-plan",
		topicservice.RoleSelect:        "campaign-plan",
		topicservice.RoleFallback:      "campaign-plan",
		briefservice.RoleInterviewer:   "campaign-context",
	}
	got := skillBindings()
	if !maps.Equal(got, want) {
		t.Errorf("skillBindings() = %v, want %v", got, want)
	}
	if _, err := skills.New(&nopClient{}, skills.Options{Bindings: got}); err != nil {
		t.Fatalf("карта ролей ссылается на отсутствующий скил: %v", err)
	}
}

type nopClient struct{}

func (nopClient) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	return corellm.Usage{}, nil
}

type captureClient struct{}

func (c *captureClient) Complete(_ context.Context, _, _, _ string, _ any) (corellm.Usage, error) {
	return corellm.Usage{}, nil
}

type captureRecorder struct{ events []trace.Event }

func (r *captureRecorder) Event(_ context.Context, ev trace.Event) { r.events = append(r.events, ev) }

func (r *captureRecorder) Enabled() bool { return true }

func TestSkillsWrapOutsideTracing(t *testing.T) {
	rec := &captureRecorder{}
	c, err := newLLMClient(&captureClient{}, rec)
	if err != nil {
		t.Fatalf("newLLMClient: %v", err)
	}
	_, err = c.Complete(context.Background(), campaignservice.RoleCopywriter, "КОНТРАКТ", "бриф", &struct{}{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("событий в трассе: %d, want 1", len(rec.events))
	}
	body, ok := rec.events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload трассы не map[string]any: %T", rec.events[0].Payload)
	}
	payload, _ := body["system"].(string)
	if !strings.Contains(payload, "name: native-article") || !strings.Contains(payload, "КОНТРАКТ") {
		t.Errorf("трасса записала промпт без скила: %q", payload)
	}
}

// proseCaveatMarker — фраза прозаической оговорки интервьюера. Продублирована
// намеренно: тест фиксирует формулировку, а не сравнение декоратора с собой.
const proseCaveatMarker = "Формат ответа задан ниже и важнее инструкций выше"

// contractCaveatMarker — фраза, которой помечен контракт JSON-роли.
const contractCaveatMarker = "Ответ — только JSON по схеме"

// streamCaptureClient — базовый клиент цепочки newLLMClient: запоминает промпты
// последнего стримингового вызова, чтобы тест увидел, что дошло до внешнего слоя.
type streamCaptureClient struct {
	gotRole, gotSystem, gotUser string
}

func (c *streamCaptureClient) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	return corellm.Usage{}, nil
}

func (c *streamCaptureClient) CompleteStream(_ context.Context, role, system, user string, _ func(string)) (corellm.Usage, error) {
	c.gotRole, c.gotSystem, c.gotUser = role, system, user
	return corellm.Usage{}, nil
}

// TestInterviewerSkillIsProse проверяет ту же цепочку, что собирает composition
// root: стрим доходит до внешнего слоя, а прозаическая роль interviewer получает
// короткую оговорку вместо контракта JSON. Тест падает, если декораторы
// перестанут пробрасывать стрим или роль потеряет прозаическую настройку.
func TestInterviewerSkillIsProse(t *testing.T) {
	base := &streamCaptureClient{}
	c, err := newLLMClient(base, nil)
	if err != nil {
		t.Fatalf("newLLMClient: %v", err)
	}
	stream, ok := c.(corellm.Streamer)
	if !ok {
		t.Fatal("цепочка декораторов не пробрасывает стрим: нет corellm.Streamer")
	}

	if _, err := stream.CompleteStream(context.Background(), briefservice.RoleInterviewer, "КОНТРАКТ ИНТЕРВЬЮ", "реплики", func(string) {}); err != nil {
		t.Fatalf("CompleteStream(interviewer): %v", err)
	}
	if base.gotRole != briefservice.RoleInterviewer {
		t.Errorf("роль вызова = %q, want %q", base.gotRole, briefservice.RoleInterviewer)
	}
	if !strings.Contains(base.gotSystem, proseCaveatMarker) {
		t.Errorf("интервьюеру не подставлена прозаическая оговорка: %q", base.gotSystem)
	}
	if strings.Contains(base.gotSystem, contractCaveatMarker) {
		t.Errorf("интервьюеру подставлен контракт JSON: %q", base.gotSystem)
	}
	if !strings.HasSuffix(base.gotSystem, "КОНТРАКТ ИНТЕРВЬЮ") {
		t.Error("контракт роли должен быть последним")
	}

	if _, err := stream.CompleteStream(context.Background(), campaignservice.RoleCopywriter, "КОНТРАКТ КОПИРАЙТЕРА", "бриф", func(string) {}); err != nil {
		t.Fatalf("CompleteStream(copywriter): %v", err)
	}
	if !strings.Contains(base.gotSystem, contractCaveatMarker) {
		t.Errorf("копирайтер потерял контракт JSON: %q", base.gotSystem)
	}
	if strings.Contains(base.gotSystem, proseCaveatMarker) {
		t.Errorf("копирайтеру подставлена прозаическая оговорка: %q", base.gotSystem)
	}
}
