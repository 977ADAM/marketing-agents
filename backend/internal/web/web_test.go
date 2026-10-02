package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/ code = %d, want 200", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/campaigns/abc-123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown route code = %d, want 200 (index fallback)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Errorf("expected content-type on fallback")
	}
}
