package http_test

import (
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	campaignhttp "github.com/977ADAM/marketing-agents/internal/features/campaign/transport/http"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	reviewhttp "github.com/977ADAM/marketing-agents/internal/features/review/transport/http"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	tracehttp "github.com/977ADAM/marketing-agents/internal/features/trace/transport/http"
)

type testRunner interface {
	campaignservice.Starter
	reviewservice.Starter
}
type testSubscriber interface {
	campaignhttp.Subscriber
	reviewhttp.Subscriber
}

func newAPI(c campaignservice.Store, r reviewservice.Store, t trace.Store, runner testRunner, sub testSubscriber, perMin int) *server.Router {
	cs := campaignservice.NewService(c, runner)
	rs := reviewservice.NewService(r, runner)
	lim := middleware.NewRateLimiter(perMin)
	router := server.New()
	router.RegisterRoutes(campaignhttp.NewHandler(cs, sub, lim).Routes()...)
	router.RegisterRoutes(reviewhttp.NewHandler(rs, sub, lim).Routes()...)
	router.RegisterRoutes(tracehttp.NewHandler(cs, rs, traceservice.NewQuery(t)).Routes()...)
	return router
}
