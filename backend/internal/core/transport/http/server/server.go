package server

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Route struct {
	Pattern string
	Handler http.HandlerFunc
}
type Router struct{ mux *http.ServeMux }

func New() *Router {
	m := http.NewServeMux()
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
