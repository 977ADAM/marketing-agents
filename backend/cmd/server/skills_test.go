package main

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/adapters/skills"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
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
