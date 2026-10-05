package campaignservice_test

import (
	"context"
	"errors"
	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	"github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	service "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	"github.com/977ADAM/marketing-agents/internal/testkit/mock"
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
	admission run.Admission
	id        string
	brief     campaign.Brief
}

func (s *starter) ExecuteCampaign(_ context.Context, id string, b campaign.Brief) {
	s.id = id
	s.brief = b
}
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
	_, err := svc.Create(context.Background(), "", testBrief())
	if err == nil || exec.id != "" {
		t.Fatalf("err=%v started=%s", err, exec.id)
	}
}

func TestRequestedTopicsOverConfiguredCapRejected(t *testing.T) {
	store := &createStore{}
	exec := &starter{}
	b := testBrief()
	b.TopicsCount = 6
	_, err := service.NewService(store, exec).Create(context.Background(), "", b)
	if err == nil || exec.id != "" || store.created.Product != "" {
		t.Fatalf("err=%v started=%s", err, exec.id)
	}
}

func (s *starter) Reserve(ctx context.Context) (run.Reservation, error) {
	if s.admission != nil {
		return s.admission.Reserve(ctx)
	}
	return mock.Reservation{}, nil
}

func TestCreateFailureReleasesReservation(t *testing.T) {
	ctx := context.Background()
	a := runner.NewAdmission(ctx, 1)
	store := &createStore{err: errors.New("db down")}
	exec := &starter{admission: a}
	_, err := service.NewService(store, exec).Create(ctx, "", testBrief())
	if err == nil {
		t.Fatal("expected DB error")
	}
	slot, err := a.Reserve(ctx)
	if err != nil {
		t.Fatalf("slot leaked: %v", err)
	}
	slot.Release()
}
func TestBusyDoesNotCreateRecord(t *testing.T) {
	ctx := context.Background()
	a := runner.NewAdmission(ctx, 1)
	slot, _ := a.Reserve(ctx)
	defer slot.Release()
	store := &createStore{}
	exec := &starter{admission: a}
	_, err := service.NewService(store, exec).Create(ctx, "", testBrief())
	if !errors.Is(err, run.ErrBusy) || store.created.Product != "" {
		t.Fatalf("busy created record, err=%v", err)
	}
}
