package reviewservice_test

import (
	"context"
	"errors"
	"github.com/977ADAM/marketing-agents/internal/core/run"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/review/service"
	"github.com/977ADAM/marketing-agents/internal/testkit/mock"
	"strings"
	"testing"
)

type createStore struct {
	service.Store
	err   error
	brief string
}

func (s *createStore) CreateCheck(_ context.Context, _ string, b string) (string, error) {
	s.brief = b
	return "rev-1", s.err
}

type starter struct {
	id  string
	req review.Request
}

func (s *starter) ExecuteReview(_ context.Context, id string, req review.Request) {
	s.id = id
	s.req = req
}
func TestCreatePersistsAndStartsReview(t *testing.T) {
	store := &createStore{}
	exec := &starter{}
	svc := service.NewService(store, exec)
	req := review.Request{BriefText: "бриф", Texts: []review.TextToReview{{Body: "текст"}}}
	id, err := svc.Create(context.Background(), "", req)
	if err != nil || id != "rev-1" || exec.id != id || store.brief != req.BriefText || exec.req.Texts[0].Body != "текст" {
		t.Fatalf("create: id=%s err=%v started=%s", id, err, exec.id)
	}
}
func TestCreateFailureDoesNotStartReview(t *testing.T) {
	store := &createStore{err: errors.New("db down")}
	exec := &starter{}
	_, err := service.NewService(store, exec).Create(context.Background(), "", review.Request{BriefText: "B", Texts: []review.TextToReview{{Body: "T"}}})
	if err == nil || exec.id != "" {
		t.Fatalf("err=%v started=%s", err, exec.id)
	}
}

func TestReviewInputLimits(t *testing.T) {
	for _, req := range []review.Request{
		{BriefText: "B", Texts: make([]review.TextToReview, 21)},
		{BriefText: "B", Texts: []review.TextToReview{{Body: strings.Repeat("x", 512*1024+1)}}},
	} {
		store := &createStore{}
		exec := &starter{}
		_, err := service.NewService(store, exec).Create(context.Background(), "", req)
		if err == nil || exec.id != "" || store.brief != "" {
			t.Fatalf("err=%v saved=%s", err, store.brief)
		}
	}
}

func (*starter) Reserve(context.Context) (run.Reservation, error) { return mock.Reservation{}, nil }
