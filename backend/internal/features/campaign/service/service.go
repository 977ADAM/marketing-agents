package campaignservice

import (
	"context"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	"strings"
)

type Starter interface{ Start(string, campaign.Brief) }
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
	id, err := s.store.Create(ctx, clientID, b)
	if err != nil {
		return "", err
	}
	s.starter.Start(id, b)
	return id, nil
}
func (s *Service) Get(ctx context.Context, id string) (*campaign.Record, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) ListRecent(ctx context.Context, limit int) ([]campaign.Summary, error) {
	return s.store.ListRecent(ctx, limit)
}
