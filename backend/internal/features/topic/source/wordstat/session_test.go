package wordstat

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/977ADAM/marketing-agents/internal/core/toolbudget"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuccessfulResponseDoesNotLoseSession(t *testing.T) {
	if sessionLost([]byte(`{"result":{"structuredContent":{"phrase":"session recording"}}}`)) {
		t.Fatal("successful result treated as session failure")
	}
}
func TestRPCSessionErrorDetected(t *testing.T) {
	if !sessionLost([]byte(`{"error":{"code":-32001,"message":"Session not found"}}`)) {
		t.Fatal("session loss missed")
	}
}

func TestLateSessionFailureDoesNotResetNewSession(t *testing.T) {
	c := New(Options{})
	c.sessionID = "new"
	c.resetSession("old")
	if c.sessionID != "new" {
		t.Fatal("new session reset")
	}
	c.resetSession("new")
	if c.sessionID != "" {
		t.Fatal("old session retained")
	}
}
func TestToolRetryConsumesBudget(t *testing.T) {
	var tools, inits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch request.Method {
		case "initialize":
			inits.Add(1)
			w.Header().Set("Mcp-Session-Id", "s")
			_, _ = w.Write([]byte(`{"result":{}}`))
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/call":
			tools.Add(1)
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	ctx, b := toolbudget.New(context.Background(), 1)
	c := New(Options{URL: server.URL, Backoff: time.Millisecond, MaxRetries: 2})
	_, err := c.callTool(ctx, "top_requests", map[string]any{})
	if !errors.Is(err, toolbudget.ErrExhausted) || tools.Load() != 1 || b.Used() != 1 || b.Initializations() != 1 || inits.Load() != 1 {
		t.Fatalf("err=%v tools=%d budget=%d init=%d", err, tools.Load(), b.Used(), inits.Load())
	}
}
func TestSuccessfulSessionPhraseMakesOneCall(t *testing.T) {
	var tools atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s")
			_, _ = w.Write([]byte(`{"result":{}}`))
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/call":
			tools.Add(1)
			_, _ = w.Write([]byte(`{"result":{"structuredContent":{"phrase":"session recording"}}}`))
		}
	}))
	defer server.Close()
	c := New(Options{URL: server.URL})
	_, err := c.callTool(context.Background(), "top_requests", map[string]any{})
	if err != nil || tools.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, tools.Load())
	}
}
