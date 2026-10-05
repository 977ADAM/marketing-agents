package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"oversize", "campaigns", `{"product":"` + strings.Repeat("x", 2<<20) + `"}`, 413},
		{"extra", "campaigns", `{"product":"P","goal":"G","audience":"A","tone":"T"}{}`, 400},
		{"too many topics", "campaigns", `{"product":"P","goal":"G","audience":"A","tone":"T","topics_count":6}`, 400},
		{"oversize text", "reviews", `{"brief":"B","texts":[{"body":"` + strings.Repeat("x", 512*1024+1) + `"}]}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{}
			runner := &mockRunner{}
			api := newAPI(repo, repo, repo, runner, nil, 100)
			w := httptest.NewRecorder()
			api.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/"+tc.path, strings.NewReader(tc.body)))
			if w.Code != tc.status || repo.created != "" {
				t.Fatalf("status=%d created=%s body=%s", w.Code, repo.created, w.Body.String())
			}
		})
	}
}
