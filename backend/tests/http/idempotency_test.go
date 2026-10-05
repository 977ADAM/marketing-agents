package http_test

import (
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
