package reviewservice_test

import (
	"context"
	"errors"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/review/service"
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

func (s *starter) StartReview(id string, req review.Request) { s.id = id; s.req = req }
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
	_, err := service.NewService(store, exec).Create(context.Background(), "", review.Request{})
	if err == nil || exec.id != "" {
		t.Fatalf("err=%v started=%s", err, exec.id)
	}
}
