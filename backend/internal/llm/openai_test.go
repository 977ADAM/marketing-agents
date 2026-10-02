package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc — подменяет http.RoundTripper в go-openai клиенте.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(model, content string, pt, ct int) *http.Response {
	body := map[string]any{
		"id":      "x",
		"object":  "chat.completion",
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": pt, "completion_tokens": ct, "total_tokens": pt + ct},
	}
	b, _ := json.Marshal(body)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{"Content-Type": []string{"application/json"}}}
}

func TestCompleteRetriesOn5xx(t *testing.T) {
	calls := 0
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"busy"}}`)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
		}
		return jsonResponse("m", `{"ok":true}`, 1, 1), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "m", 2, &http.Client{Transport: rt})

	var out struct {
		OK bool `json:"ok"`
	}
	if _, err := c.Complete(context.Background(), "any", "s", "u", &out); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", calls)
	}
	if !out.OK {
		t.Error("out.OK = false")
	}
}

func TestCompleteParsesJSONAndUsage(t *testing.T) {
	var gotModel string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct{ Model string `json:"model"` }
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		gotModel = payload.Model
		return jsonResponse(payload.Model, `{"score":42}`, 10, 5), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "model-default", 1, &http.Client{Transport: rt})

	var out struct {
		Score int `json:"score"`
	}
	u, err := c.Complete(context.Background(), "strategist", "sys", "user", &out)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out.Score != 42 {
		t.Errorf("out.Score = %d, want 42", out.Score)
	}
	if u.PromptTokens != 10 || u.CompletionTokens != 5 {
		t.Errorf("usage = %+v", u)
	}
	if gotModel != "model-default" {
		t.Errorf("model = %q, want default", gotModel)
	}
}

// statusResponse — ответ с произвольным кодом и телом (для веток ошибок).
func statusResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestCompleteNoRetryOn4xx(t *testing.T) {
	calls := 0
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return statusResponse(400, `{"error":{"message":"bad request"}}`), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "m", 3, &http.Client{Transport: rt})

	var out struct{}
	if _, err := c.Complete(context.Background(), "any", "s", "u", &out); err == nil {
		t.Fatal("Complete: ожидалась ошибка на 400")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (4xx не ретраим)", calls)
	}
}

func TestCompleteExhaustsRetries(t *testing.T) {
	calls := 0
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return statusResponse(503, `{"error":{"message":"busy"}}`), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "m", 1, &http.Client{Transport: rt})

	var out struct{}
	_, err := c.Complete(context.Background(), "any", "s", "u", &out)
	if err == nil {
		t.Fatal("Complete: ожидалась ошибка после исчерпания ретраев")
	}
	if !strings.Contains(err.Error(), "exhausted retries") {
		t.Errorf("err = %q, want содержит «exhausted retries»", err.Error())
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (1 попытка + 1 ретрай)", calls)
	}
}

func TestRoleModelOverride(t *testing.T) {
	var gotModel string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Model string `json:"model"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		gotModel = payload.Model
		return jsonResponse(payload.Model, `{"ok":true}`, 1, 1), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "model-default", 1, &http.Client{Transport: rt})
	c.SetRoleModel("copywriter", "model-fast")

	var out struct {
		OK bool `json:"ok"`
	}
	if _, err := c.Complete(context.Background(), "copywriter", "s", "u", &out); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotModel != "model-fast" {
		t.Errorf("model = %q, want model-fast (override по роли)", gotModel)
	}
}

func TestCompleteEmptyChoices(t *testing.T) {
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return statusResponse(200, `{"id":"x","object":"chat.completion","model":"m","choices":[],"usage":{}}`), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "m", 1, &http.Client{Transport: rt})

	var out struct{}
	_, err := c.Complete(context.Background(), "any", "s", "u", &out)
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("err = %v, want содержит «empty choices»", err)
	}
}

func TestCompleteBadJSONContent(t *testing.T) {
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse("m", `не json`, 3, 2), nil
	})
	c := New("sk", "https://api.deepseek.com/v1", "m", 1, &http.Client{Transport: rt})

	var out struct {
		Score int `json:"score"`
	}
	u, err := c.Complete(context.Background(), "any", "s", "u", &out)
	if err == nil || !strings.Contains(err.Error(), "parse JSON") {
		t.Fatalf("err = %v, want содержит «parse JSON»", err)
	}
	// usage возвращается даже при ошибке парсинга
	if u.PromptTokens != 3 || u.CompletionTokens != 2 {
		t.Errorf("usage = %+v, want {3 2}", u)
	}
}
