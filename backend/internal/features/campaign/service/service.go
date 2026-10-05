package campaignservice

import (
	"context"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
)

type Starter interface{ Start(string, campaign.Brief) }
type Service struct {
	store   Store
	starter Starter
}

func NewService(store Store, starter Starter) *Service { return &Service{store, starter} }
func (s *Service) Create(ctx context.Context, clientID string, b campaign.Brief) (string, error) {
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
