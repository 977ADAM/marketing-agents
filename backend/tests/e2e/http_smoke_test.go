package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/977ADAM/marketing-agents/internal/adapters/accounting"
	sloglogger "github.com/977ADAM/marketing-agents/internal/adapters/logger/slog"
	tracing "github.com/977ADAM/marketing-agents/internal/adapters/tracing"
	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	campaignrepo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	campaignhttp "github.com/977ADAM/marketing-agents/internal/features/campaign/transport/http"
	reviewrepo "github.com/977ADAM/marketing-agents/internal/features/review/repository/mariadb"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	tracerepo "github.com/977ADAM/marketing-agents/internal/features/trace/repository/mariadb"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	mock "github.com/977ADAM/marketing-agents/internal/testkit/mock"
	testdb "github.com/977ADAM/marketing-agents/internal/testkit/testdb"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompositionHTTPAndSSESmoke(t *testing.T) {
	ctx := context.Background()
	db, _ := testdb.New(t)
	campaigns := campaignrepo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	events := tracerepo.NewEvents(db)
	rec := traceservice.New(events, trace.Config{Mode: trace.ModeFull})
	fake := mock.NewLLM()
	fake.Responses[campaignservice.RoleStrategist] = []string{`{"positioning":"p","topics":[{"title":"t","angle":"a","points":["x"]}]}`}
	fake.Responses[campaignservice.RoleCopywriter] = []string{`{"topic":"t","title":"article","body":"body","cta":"c"}`}
	client := accounting.New(tracing.NewLLM(fake, rec))
	wf := campaignservice.NewWorkflow(client, campaignservice.Options{Checkpoints: campaigns, Recorder: rec})
	hub := runner.NewHub(ctx, campaigns, reviews)
	background := runner.NewRunner(ctx, campaigns, reviews, wf, reviewservice.NewWorkflow(client, reviewservice.Options{Checkpoints: reviews, Recorder: rec}), 5*time.Second, sloglogger.New(slog.New(slog.NewTextHandler(io.Discard, nil))), hub)
	defer background.Drain()
	svc := campaignservice.NewService(campaigns, background)
	api := server.New()
	api.RegisterRoutes(campaignhttp.NewHandler(svc, hub, middleware.NewRateLimiter(0)).Routes()...)
	httpServer := httptest.NewServer(api.Handler())
	defer httpServer.Close()
	httpClient := &http.Client{Timeout: 10 * time.Second}
	create := func() string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/campaigns", bytes.NewBufferString(`{"product":"p","goal":"g","audience":"a","tone":"t","topics_count":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "smoke")
		res, e := httpClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != 202 {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("create %d %s", res.StatusCode, body)
		}
		var out struct{ ID string }
		if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		return out.ID
	}
	id := create()
	if create() != id {
		t.Fatal("duplicate run")
	}
	res, e := httpClient.Get(httpServer.URL + "/api/campaigns/" + id + "/events")
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(res.Body)
	res.Body.Close()
	if e != nil || !strings.Contains(string(body), "event: done") {
		t.Fatalf("SSE %s %v", body, e)
	}
	res, e = httpClient.Get(httpServer.URL + "/api/campaigns/" + id)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var result campaign.Record
	if e = json.NewDecoder(res.Body).Decode(&result); e != nil || result.Status != "done" || len(result.Deliverables) != 1 || result.Usage == nil {
		t.Fatalf("result %+v %v", result, e)
	}
}
