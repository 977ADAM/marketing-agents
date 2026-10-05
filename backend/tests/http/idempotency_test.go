package http_test

import (
	"context"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	campaignrepo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	reviewrepo "github.com/977ADAM/marketing-agents/internal/features/review/repository/mariadb"
	tracerepo "github.com/977ADAM/marketing-agents/internal/features/trace/repository/mariadb"
	"github.com/977ADAM/marketing-agents/internal/testkit/testdb"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateIdempotencyHTTP(t *testing.T) {
	db, _ := testdb.New(t)
	runner := &mockRunner{called: make(chan string, 4)}
	api := newAPI(campaignrepo.NewCampaigns(db), reviewrepo.NewReviews(db), tracerepo.NewEvents(db), runner, nil, 100)
	for _, path := range []string{"campaigns", "reviews"} {
		body := `{"product":"P","goal":"G","audience":"A","tone":"T"}`
		changed := `{"product":"Q","goal":"G","audience":"A","tone":"T"}`
		if path == "reviews" {
			body = `{"brief":"B","texts":[{"body":"T"}]}`
			changed = `{"brief":"Q","texts":[{"body":"T"}]}`
		}
		post := func(body string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("POST", "/api/"+path, strings.NewReader(body))
			r.Header.Set("Idempotency-Key", "same")
			w := httptest.NewRecorder()
			api.Handler().ServeHTTP(w, r)
			return w
		}
		a, b := post(body), post(body)
		if a.Code != 202 || b.Code != 202 || a.Body.String() != b.Body.String() {
			t.Fatalf("responses=%d %d %s %s", a.Code, b.Code, a.Body, b.Body)
		}
		if w := post(changed); w.Code != 409 {
			t.Fatalf("conflict=%d %s", w.Code, w.Body)
		}
	}
	if len(runner.called) != 2 {
		t.Fatalf("started=%d", len(runner.called))
	}
}

func TestRetryStateResponses(t *testing.T) {
	db, _ := testdb.New(t)
	ctx := context.Background()
	campaigns := campaignrepo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	runner := &mockRunner{called: make(chan string, 4)}
	api := newAPI(campaigns, reviews, tracerepo.NewEvents(db), runner, nil, 100)
	id, err := campaigns.Create(ctx, "", campaign.Brief{Product: "P"})
	if err != nil {
		t.Fatal(err)
	}
	_ = campaigns.Fail(ctx, id, "failed")
	post := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		return w
	}
	if w := post("/api/campaigns/" + id + "/retry"); w.Code != 202 {
		t.Fatalf("retry=%d %s", w.Code, w.Body)
	}
	if w := post("/api/campaigns/" + id + "/retry"); w.Code != 409 || !strings.Contains(w.Body.String(), "invalid_state") {
		t.Fatalf("running retry=%d %s", w.Code, w.Body)
	}
	old, err := reviews.CreateCheck(ctx, "", "old")
	if err != nil {
		t.Fatal(err)
	}
	_ = reviews.FailCheck(ctx, old, "failed")
	if w := post("/api/reviews/" + old + "/retry"); w.Code != 409 || !strings.Contains(w.Body.String(), "resume_unavailable") {
		t.Fatalf("legacy=%d %s", w.Code, w.Body)
	}
}
