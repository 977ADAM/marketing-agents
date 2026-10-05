package server

import (
	"context"
	"errors"
	"github.com/977ADAM/marketing-agents/internal/core/limits"
	"github.com/977ADAM/marketing-agents/internal/core/transport/http/response"
	"net/http"
	"time"
)

type Route struct {
	Pattern string
	Handler http.HandlerFunc
}
type Router struct{ mux *http.ServeMux }

func New(opt ...limits.Limits) *Router {
	l := limits.Defaults()
	if len(opt) > 0 {
		l = limits.Normalize(opt[0])
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /api/limits", func(w http.ResponseWriter, r *http.Request) {
		response.WriteJSON(w, http.StatusOK, map[string]any{"max_topics": l.MaxTopics, "max_texts": l.MaxTexts, "max_text_bytes": l.MaxTextBytes})
	})
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return &Router{m}
}
func (r *Router) RegisterRoutes(routes ...Route) {
	for _, route := range routes {
		r.mux.HandleFunc(route.Pattern, route.Handler)
	}
}
func (r *Router) Handler() http.Handler { return r.mux }

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
}
type HTTPServer struct {
	server          *http.Server
	shutdownTimeout time.Duration
}

func NewHTTPServer(cfg Config, h http.Handler) *HTTPServer {
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}
	return &HTTPServer{&http.Server{Addr: cfg.Addr, Handler: h}, cfg.ShutdownTimeout}
}
func (s *HTTPServer) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return s.server.Shutdown(shut)
	}
}
