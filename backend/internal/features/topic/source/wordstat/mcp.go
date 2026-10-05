package wordstat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/toolbudget"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// protocolVersion — ревизия MCP, которую объявляет клиент.
const protocolVersion = "2025-06-18"

// maxErrBody ограничивает размер тела ошибки, попадающего в сообщение.
const maxErrBody = 512

// Options — настройки клиента MCP.
type Options struct {
	URL  string
	User string
	Pass string
	// HTTP — подмена транспорта в тестах (nil — дефолтный с таймаутом 60s).
	HTTP *http.Client
	// MaxRetries — сколько повторов на retryable-ошибки (по умолчанию 2).
	MaxRetries int
	// Backoff — базовая пауза перед повтором, удваивается (по умолчанию 500ms).
	Backoff time.Duration
}

// Client — клиент Wordstat через MCP (Streamable HTTP, JSON-RPC).
//
// Сессия MCP создаётся лениво при первом вызове и переиспользуется: initialize →
// notifications/initialized → tools/call. Методы безопасны для параллельных
// вызовов (сеялки обрабатываются конкурентно).
type Client struct {
	url        string
	user, pass string
	http       *http.Client
	maxRetries int
	backoff    time.Duration

	mu        sync.Mutex // защищает sessionID
	sessionID string
	ids       atomic.Int64
}

// New создаёт клиент MCP.
func New(opt Options) *Client {
	c := &Client{
		url: opt.URL, user: opt.User, pass: opt.Pass, http: opt.HTTP,
		maxRetries: opt.MaxRetries, backoff: opt.Backoff,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 60 * time.Second}
	}
	if c.maxRetries <= 0 {
		c.maxRetries = 2
	}
	if c.backoff <= 0 {
		c.backoff = 500 * time.Millisecond
	}
	return c
}

// --- JSON-RPC ---

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// toolResult — результат tools/call по протоколу MCP.
type toolResult struct {
	IsError           bool            `json:"isError"`
	Content           []toolContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolStatus — тело структурированной ошибки инструмента (isError = true).
type toolStatus struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable *bool  `json:"retryable"`
}

// --- HTTP ---

// post отправляет JSON-RPC запрос. Тело ответа и статус возвращаются как есть:
// решение о ретрае принимает вызывающая сторона.
func (c *Client) post(ctx context.Context, payload any, sessionID string) ([]byte, int, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: кодирование запроса: %v", ErrInternal, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: создание запроса: %v", ErrInternal, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	if rpc, ok := payload.(rpcRequest); ok {
		if rpc.Method == "tools/call" {
			if err := toolbudget.Consume(ctx); err != nil {
				return nil, 0, "", err
			}
		}
		if rpc.Method == "initialize" {
			toolbudget.Initialization(ctx)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// Сетевые сбои и таймауты — временные: повтор имеет смысл.
		return nil, 0, "", fmt.Errorf("%w: запрос к MCP: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, "", fmt.Errorf("%w: чтение ответа MCP: %v", ErrUnavailable, err)
	}
	return data, resp.StatusCode, resp.Header.Get("Mcp-Session-Id"), nil
}

// --- сессия ---

// ensureSession возвращает действующую сессию, создавая её при необходимости.
func (c *Client) ensureSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID != "" {
		return c.sessionID, nil
	}
	return c.initializeLocked(ctx)
}

// initializeLocked выполняет initialize + notifications/initialized.
// Вызывается с захваченным mu.
func (c *Client) initializeLocked(ctx context.Context) (string, error) {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "marketing-agents", "version": "1.0"},
	}
	body, status, sid, err := c.post(ctx, rpcRequest{
		JSONRPC: "2.0", ID: c.ids.Add(1), Method: "initialize", Params: params,
	}, "")
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "", fmt.Errorf("%w: MCP отклонил авторизацию (HTTP %d)", ErrInternal, status)
	case status < 200 || status > 299:
		return "", fmt.Errorf("%w: initialize не удался (HTTP %d): %s", ErrUnavailable, status, short(body))
	}
	if _, err := parseRPCResult(body); err != nil {
		return "", err
	}
	if sid == "" {
		return "", fmt.Errorf("%w: MCP не выдал Mcp-Session-Id", ErrInternal)
	}

	// Обязательный шаг протокола: уведомление о готовности клиента.
	if _, status, _, err := c.post(ctx, rpcRequest{
		JSONRPC: "2.0", Method: "notifications/initialized",
	}, sid); err != nil {
		return "", err
	} else if status < 200 || status > 299 {
		return "", fmt.Errorf("%w: notifications/initialized не удался (HTTP %d)", ErrUnavailable, status)
	}

	c.sessionID = sid
	return sid, nil
}

// resetSession сбрасывает сессию: MCP мог её потерять (рестарт, истечение).
func (c *Client) resetSession(stale string) {
	c.mu.Lock()
	if c.sessionID == stale {
		c.sessionID = ""
	}
	c.mu.Unlock()
}

// --- вызовы инструментов ---

// callTool вызывает инструмент с повторами на retryable-ошибки.
func (c *Client) callTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, c.backoff<<uint(attempt-1)); err != nil {
				return nil, err
			}
		}
		raw, err := c.callToolOnce(ctx, name, args)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !Retryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("wordstat: %s: повторы исчерпаны: %w", name, lastErr)
}

// callToolOnce — один вызов tools/call с переподключением при потере сессии.
func (c *Client) callToolOnce(ctx context.Context, name string, args any) (json.RawMessage, error) {
	sid, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	req := rpcRequest{
		JSONRPC: "2.0", ID: c.ids.Add(1), Method: "tools/call",
		Params: map[string]any{"name": name, "arguments": args},
	}
	body, status, _, err := c.post(ctx, req, sid)
	if err != nil {
		return nil, err
	}

	// Сессия могла истечь на стороне MCP — переподключаемся и пробуем ещё раз.
	if status == http.StatusNotFound || sessionLost(body) {
		c.resetSession(sid)
		sid, err = c.ensureSession(ctx)
		if err != nil {
			return nil, err
		}
		if body, status, _, err = c.post(ctx, req, sid); err != nil {
			return nil, err
		}
	}

	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, fmt.Errorf("%w: MCP отклонил авторизацию (HTTP %d)", ErrInternal, status)
	case status < 200 || status > 299:
		return nil, fmt.Errorf("%w: %s: HTTP %d: %s", ErrUnavailable, name, status, short(body))
	}

	raw, err := parseRPCResult(body)
	if err != nil {
		return nil, err
	}

	var res toolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("%w: разбор результата %s: %v", ErrInternal, name, err)
	}
	if res.IsError {
		var st toolStatus
		_ = json.Unmarshal(res.StructuredContent, &st)
		return nil, toolError(st.Code, st.Message)
	}
	if len(res.StructuredContent) == 0 {
		return nil, fmt.Errorf("%w: %s вернул пустой structuredContent", ErrInternal, name)
	}
	return res.StructuredContent, nil
}

// parseRPCResult достаёт поле result из JSON-RPC ответа (в SSE-обёртке или как есть).
func parseRPCResult(body []byte) (json.RawMessage, error) {
	payload, err := extractJSON(body)
	if err != nil {
		return nil, err
	}
	var resp rpcResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("%w: разбор JSON-RPC ответа: %v", ErrInternal, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%w: MCP %d: %s", ErrInternal, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// extractJSON возвращает JSON из тела ответа: либо как есть, либо из SSE-обёртки
// (Streamable HTTP отдаёт text/event-stream со строками `data: {...}`).
func extractJSON(body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return trimmed, nil
	}
	var data []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimSpace(rest))
			continue
		}
		if strings.TrimSpace(line) == "" && len(data) > 0 {
			break // конец первого события с данными
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: в ответе MCP нет data-строки", ErrInternal)
	}
	return []byte(strings.Join(data, "\n")), nil
}

// sessionLost сообщает, что MCP потерял сессию.
func sessionLost(body []byte) bool {
	payload, err := extractJSON(body)
	if err != nil {
		return false
	}
	var rpc rpcResponse
	if json.Unmarshal(payload, &rpc) != nil {
		return false
	}
	var message string
	if rpc.Error != nil {
		message = rpc.Error.Message
	} else {
		var result toolResult
		if json.Unmarshal(rpc.Result, &result) != nil || !result.IsError {
			return false
		}
		var status toolStatus
		if json.Unmarshal(result.StructuredContent, &status) != nil {
			return false
		}
		switch status.Code {
		case "session_not_found", "session_expired", "invalid_session":
			return true
		}
		message = status.Message
	}
	message = strings.ToLower(strings.TrimSpace(message))
	return message == "session not found" || message == "session expired" || message == "invalid session" || message == "missing session"
}

// short обрезает тело для сообщения об ошибке.
func short(body []byte) string {
	msg := strings.TrimSpace(string(body))
	if len(msg) > maxErrBody {
		return msg[:maxErrBody] + "…"
	}
	return msg
}

// sleepCtx ждёт паузу или отмену контекста.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// --- инструменты ---

// TopRequests возвращает спрос по фразе (инструмент top_requests).
func (c *Client) topRequests(ctx context.Context, p TopParams) (*Top, error) {
	if strings.TrimSpace(p.Phrase) == "" {
		return nil, fmt.Errorf("%w: пустая фраза", ErrInvalidArgument)
	}
	args := map[string]any{"phrase": p.Phrase}
	if p.NumPhrases > 0 {
		args["numPhrases"] = p.NumPhrases
	}
	if len(p.Regions) > 0 {
		args["regions"] = p.Regions
	}
	if len(p.Devices) > 0 {
		args["devices"] = p.Devices
	}
	return callTyped[Top](ctx, c, "top_requests", args)
}

// Dynamics возвращает сезонность фразы (инструмент dynamics).
func (c *Client) fetchDynamics(ctx context.Context, p DynamicsParams) (*Dynamics, error) {
	if strings.TrimSpace(p.Phrase) == "" {
		return nil, fmt.Errorf("%w: пустая фраза", ErrInvalidArgument)
	}
	args := map[string]any{"phrase": p.Phrase}
	if p.Period != "" {
		args["period"] = p.Period
	}
	if p.FromDate != "" {
		args["fromDate"] = p.FromDate
	}
	if p.ToDate != "" {
		args["toDate"] = p.ToDate
	}
	if len(p.Regions) > 0 {
		args["regions"] = p.Regions
	}
	if len(p.Devices) > 0 {
		args["devices"] = p.Devices
	}
	return callTyped[Dynamics](ctx, c, "dynamics", args)
}

// Regions возвращает географию спроса (инструмент regions).
func (c *Client) Regions(ctx context.Context, p RegionsParams) (*Regions, error) {
	if strings.TrimSpace(p.Phrase) == "" {
		return nil, fmt.Errorf("%w: пустая фраза", ErrInvalidArgument)
	}
	args := map[string]any{"phrase": p.Phrase}
	if p.RegionMode != "" {
		args["regionMode"] = p.RegionMode
	}
	if p.IncludeNames {
		args["includeNames"] = true
	}
	return callTyped[Regions](ctx, c, "regions", args)
}

// callTyped вызывает инструмент и разбирает structuredContent в T.
func callTyped[T any](ctx context.Context, c *Client, name string, args any) (*T, error) {
	raw, err := c.callTool(ctx, name, args)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: разбор structuredContent %s: %v", ErrInternal, name, err)
	}
	return &out, nil
}

// ConsumesToolBudget reports that retries are accounted for at the HTTP boundary.
func (*Client) ConsumesToolBudget() bool { return true }
