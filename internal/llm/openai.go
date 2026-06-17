package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

	if err := json.Unmarshal([]byte(content), out); err != nil {
		return usage, fmt.Errorf("llm: parse JSON: %w (content=%q)", err, content)
	}
	return usage, nil
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
