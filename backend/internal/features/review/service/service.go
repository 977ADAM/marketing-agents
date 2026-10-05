package reviewservice

import (
	"context"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

type Starter interface{ StartReview(string, review.Request) }
type Service struct {
	store   Store
	starter Starter
}

func NewService(store Store, starter Starter) *Service { return &Service{store, starter} }
func (s *Service) Create(ctx context.Context, clientID string, req review.Request) (string, error) {
	id, err := s.store.CreateCheck(ctx, clientID, req.BriefText)
	if err != nil {
		return "", err
	}
	s.starter.StartReview(id, req)
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
