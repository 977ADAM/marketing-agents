package reviewservice

import (
	"context"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	"github.com/977ADAM/marketing-agents/internal/core/run"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	"strings"
	"time"
)

type Starter interface {
	run.Admission
	ExecuteReview(context.Context, string, review.Request)
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
func (s *Service) Create(ctx context.Context, clientID string, req review.Request) (string, error) {
	if strings.TrimSpace(req.BriefText) == "" || len(req.BriefText) > s.limits.MaxTextBytes {
		return "", limits.Invalid("brief is empty or exceeds byte limit")
	}
	if len(req.Texts) == 0 || len(req.Texts) > s.limits.MaxTexts {
		return "", limits.Invalid("texts count must be between 1 and %d", s.limits.MaxTexts)
	}
	for i, t := range req.Texts {
		if strings.TrimSpace(t.Body) == "" || len(t.Body) > s.limits.MaxTextBytes || len(t.Title) > s.limits.MaxTextBytes {
			return "", limits.Invalid("text #%d is empty or exceeds byte limit", i+1)
		}
	}
	key := run.IdempotencyKey(ctx)
	hash := run.InputHash(req)
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
		id, created, err = once.CreateOnce(ctx, clientID, key, hash, req)
	} else {
		id, err = s.store.CreateCheck(ctx, clientID, req.BriefText)
	}
	if err != nil {
		return "", err
	}
	if !created {
		return id, nil
	}
	if err := slot.Submit(func(ctx context.Context) { s.starter.ExecuteReview(ctx, id, req) }); err != nil {
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.FailCheck(final, id, "runner stopping before submission")
		return "", err
	}
	return id, nil
}
func (s *Service) GetCheck(ctx context.Context, id string) (*review.Record, error) {
	return s.store.GetCheck(ctx, id)
}
func (s *Service) ListChecks(ctx context.Context, limit int) ([]review.Summary, error) {
	items, err := s.store.ListChecks(ctx, limit)
	if err != nil {
		return nil, err
	}
	for j := range items {
		items[j].BriefTitle = firstLine(items[j].BriefText)
	}
	return items, nil
}
func firstLine(s string) string {
	for j, r := range s {
		if r == '\n' {
			return s[:j]
		}
	}
	return s
}
