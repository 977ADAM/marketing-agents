package campaignservice_test

import (
	"context"
	"errors"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	"testing"
)

type createStore struct {
	service.Store
	err     error
	created campaign.Brief
}

func (s *createStore) Create(_ context.Context, _ string, b campaign.Brief) (string, error) {
	s.created = b
	return "run-1", s.err
}

type starter struct {
	id    string
	brief campaign.Brief
}

func (s *starter) Start(id string, b campaign.Brief) { s.id = id; s.brief = b }
func TestCreatePersistsAndStartsCampaign(t *testing.T) {
	store := &createStore{}
	exec := &starter{}
	svc := service.NewService(store, exec)
	b := campaign.Brief{Product: "Бутылка", Goal: "Продажи", Audience: "Спортсмены", Tone: "Дружелюбный"}
	id, err := svc.Create(context.Background(), "", b)
	if err != nil || id != "run-1" || exec.id != id || exec.brief != b || store.created != b {
		t.Fatalf("create: id=%s err=%v started=%s saved=%+v", id, err, exec.id, store.created)
	}
}
func TestCreateFailureDoesNotStartCampaign(t *testing.T) {
	store := &createStore{err: errors.New("db down")}
	exec := &starter{}
	svc := service.NewService(store, exec)
	_, err := svc.Create(context.Background(), "", campaign.Brief{})
	if err == nil || exec.id != "" {
		t.Fatalf("err=%v started=%s", err, exec.id)
	}
}
