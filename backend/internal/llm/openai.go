package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type OpenAIClient struct {
	api        *openai.Client
	roleModel  map[string]string
	defModel   string
	maxRetries int
}

// New собирает клиента под DeepSeek base URL. httpClient можно подменить в тестах
// (передать nil для дефолтного).
func New(apiKey, baseURL, defaultModel string, maxRetries int, httpClient *http.Client) *OpenAIClient {
	conf := openai.DefaultConfig(apiKey)
	conf.BaseURL = baseURL
	if httpClient != nil {
		conf.HTTPClient = httpClient
	}
	return &OpenAIClient{
		api:        openai.NewClientWithConfig(conf),
		roleModel:  map[string]string{},
		defModel:   defaultModel,
		maxRetries: maxRetries,
	}
}

// SetRoleModel переопределяет модель для роли (для будущего разнесения моделей).
func (c *OpenAIClient) SetRoleModel(role, model string) { c.roleModel[role] = model }

func (c *OpenAIClient) modelFor(role string) string {
	if m, ok := c.roleModel[role]; ok {
		return m
	}
	return c.defModel
}

func (c *OpenAIClient) Complete(ctx context.Context, role, system, user string, out any) (Usage, error) {
	req := openai.ChatCompletionRequest{
		Model: c.modelFor(role),
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: user},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject},
	}

	resp, err := c.callWithRetry(ctx, req)
	if err != nil {
		return Usage{}, err
	}
	if len(resp.Choices) == 0 {
		return Usage{}, errors.New("llm: empty choices")
	}
	content := resp.Choices[0].Message.Content
	usage := Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}

	if err := decodeJSON(content, out); err != nil {
		return usage, fmt.Errorf("llm: parse JSON: %w (content=%q)", err, truncate(content, 400))
	}
	return usage, nil
}

// decodeJSON разбирает ответ модели в out, терпя типичные вольности: обёртку
// вида {"type":"json_object"} рядом с настоящим ответом, пояснения до или после
// JSON, markdown-ограждения. Наблюдалось на живом прогоне: модель возвращала эхо
// response_format, и строгий json.Unmarshal валил весь прогон.
//
// Кандидаты перебираются от самого длинного к самому короткому: полезная
// нагрузка почти всегда крупнее обёртки, а порядок значений в ответе модели не
// гарантирован.
func decodeJSON(content string, out any) error {
	values := jsonValues(content)
	if len(values) == 0 {
		return fmt.Errorf("no JSON value found")
	}
	sort.SliceStable(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })

	var lastErr error
	for _, candidate := range values {
		if isFormatEcho(candidate) {
			continue // эхо response_format — не ответ модели
		}
		if err := json.Unmarshal(candidate, out); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		return fmt.Errorf("only response_format echo found, no payload")
	}
	return fmt.Errorf("no JSON object with the expected shape: %w", lastErr)
}

// isFormatEcho распознаёт объект вида {"type":"json_object"} — служебное эхо
// response_format, которое некоторые провайдеры подмешивают в content.
func isFormatEcho(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 1 {
		return false
	}
	value, ok := fields["type"]
	if !ok {
		return false
	}
	var kind string
	return json.Unmarshal(value, &kind) == nil && kind == "json_object"
}

// jsonValues вытаскивает все JSON-значения верхнего уровня из текста модели,
// начиная с первой скобки (до неё могут идти пояснения).
func jsonValues(content string) []json.RawMessage {
	start := strings.IndexAny(content, "{[")
	if start < 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(content[start:]))
	var values []json.RawMessage
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		values = append(values, raw)
	}
	return values
}

// truncate обрезает длинный текст для сообщения об ошибке.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

// callWithRetry повторяет вызов с экспоненциальным backoff на временные ошибки.
func (c *OpenAIClient) callWithRetry(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return openai.ChatCompletionResponse{}, ctx.Err()
			case <-time.After(backoff):
			}
		}
		resp, err := c.api.CreateChatCompletion(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retryable(err) {
			return openai.ChatCompletionResponse{}, err
		}
	}
	return openai.ChatCompletionResponse{}, fmt.Errorf("llm: exhausted retries: %w", lastErr)
}

// retryable — 429 и 5xx считаем временными.
func retryable(err error) bool {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatusCode == http.StatusTooManyRequests || apiErr.HTTPStatusCode >= 500
	}
	return true // сетевые ошибки тоже ретраим
}
