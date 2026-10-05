package campaignservice

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	"github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	"strings"
	"time"
)

type Starter interface {
	run.Admission
	ExecuteCampaign(context.Context, string, campaign.Brief)
}
type Service struct {
	store   Store
	starter Starter
	limits  limits.Limits
}

func NewService(store Store, starter Starter, opt ...limits.Limits) *Service {
	l := limits.Defaults()
	if len(opt) > 0 {
		l = limits.Normalize(opt[0])
	}
	return &Service{store: store, starter: starter, limits: l}
}
func (s *Service) Create(ctx context.Context, clientID string, b campaign.Brief) (string, error) {
	if strings.TrimSpace(b.Product) == "" || strings.TrimSpace(b.Goal) == "" || strings.TrimSpace(b.Audience) == "" || strings.TrimSpace(b.Tone) == "" {
		return "", limits.Invalid("product, goal, audience, tone are required")
	}
	if b.TopicsCount < 0 || b.TopicsCount > s.limits.MaxTopics {
		return "", limits.Invalid("topics_count must be between 0 and %d", s.limits.MaxTopics)
	}
	for _, v := range []string{b.Product, b.Goal, b.Audience, b.Tone} {
		if len(v) > s.limits.MaxTextBytes {
			return "", limits.Invalid("brief field exceeds byte limit")
		}
	}
	if b.Region != "" {
		for _, r := range b.Region {
			if r < '0' || r > '9' {
				return "", limits.Invalid("region must be a numeric geo id")
			}
		}
	}
	key := run.IdempotencyKey(ctx)
	hash := run.InputHash(b)
	var once CreationStore
	if key != "" {
		if len(key) > 128 {
			return "", limits.Invalid("Idempotency-Key exceeds 128 bytes")
		}
		for _, c := range key {
			if c < 33 || c > 126 {
				return "", limits.Invalid("Idempotency-Key must contain printable ASCII")
			}
		}
		var ok bool
		once, ok = s.store.(CreationStore)
		if !ok {
			return "", fmt.Errorf("store does not support idempotency")
		}
		if id, err := once.LookupCreation(ctx, clientID, key, hash); err != nil || id != "" {
			return id, err
		}
	}
	slot, err := s.starter.Reserve(ctx)
	if err != nil {
		return "", err
	}
	defer slot.Release()
	var id string
	created := true
	if once != nil {
		id, created, err = once.CreateOnce(ctx, clientID, key, hash, b)
	} else {
		id, err = s.store.Create(ctx, clientID, b)
	}
	if err != nil {
		return "", err
	}
	if !created {
		return id, nil
	}
	if err := slot.Submit(func(ctx context.Context) { s.starter.ExecuteCampaign(ctx, id, b) }); err != nil {
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.Fail(final, id, "runner stopping before submission")
		return "", err
	}
	return id, nil
}
func (s *Service) Get(ctx context.Context, id string) (*campaign.Record, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) ListRecent(ctx context.Context, limit int) ([]campaign.Summary, error) {
	return s.store.ListRecent(ctx, limit)
}

func (s *Service) Resume(ctx context.Context, id string) (string, error) {
	record, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if record.Status != "failed" {
		return "", run.ErrInvalidState
	}
	checkpoints, ok := s.store.(run.ResumeStore)
	if !ok {
		return "", run.ErrResumeUnavailable
	}
	var input campaign.Brief
	found, err := checkpoints.LoadCheckpoint(ctx, id, "input", 0, &input)
	if err != nil {
		return "", err
	}
	if !found {
		return "", run.ErrResumeUnavailable
	}
	slot, err := s.starter.Reserve(ctx)
	if err != nil {
		return "", err
	}
	defer slot.Release()
	if err := checkpoints.Requeue(ctx, id); err != nil {
		return "", err
	}
	if err := slot.Submit(func(ctx context.Context) { s.starter.ExecuteCampaign(ctx, id, input) }); err != nil {
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.Fail(final, id, "runner stopping before retry submission")
		return "", err
	}
	return id, nil
}
