package wordstat

import (
	"errors"
	"fmt"
)

// Sentinel-ошибки: вызывающая сторона по ним решает, повторять ли запрос и что
// показать пользователю. Оборачиваются через %w, проверяются errors.Is.
var (
	// ErrInvalidArgument — некорректный запрос (регион, фраза, аргументы).
	// Повтор не поможет: это ошибка ввода.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrQuotaExceeded — исчерпана квота Wordstat. Повтор имеет смысл позже.
	ErrQuotaExceeded = errors.New("quota exceeded")
	// ErrUnavailable — MCP-сервис недоступен: сеть, 5xx, обрыв сессии.
	ErrUnavailable = errors.New("wordstat unavailable")
	// ErrInternal — всё остальное: 401/403, ошибки протокола MCP.
	ErrInternal = errors.New("internal error")
)

// Коды ошибок инструментов MCP-сервера (его structuredContent при isError=true).
const (
	codeInvalidArgument     = "invalid_argument"
	codeQuotaExceeded       = "quota_exceeded"
	codeUpstreamUnavailable = "upstream_unavailable"
	codeInternal            = "internal"
)

// Retryable сообщает, имеет ли смысл повторить запрос.
func Retryable(err error) bool {
	return errors.Is(err, ErrQuotaExceeded) || errors.Is(err, ErrUnavailable)
}

// sentinelForCode переводит код ошибки инструмента MCP в sentinel.
func sentinelForCode(code string) error {
	switch code {
	case codeInvalidArgument:
		return ErrInvalidArgument
	case codeQuotaExceeded:
		return ErrQuotaExceeded
	case codeUpstreamUnavailable:
		return ErrUnavailable
	case codeInternal:
		return ErrInternal
	default:
		return ErrInternal
	}
}

// toolError формирует ошибку инструмента, сохраняя текст от MCP.
func toolError(code, message string) error {
	sentinel := sentinelForCode(code)
	if message == "" {
		return fmt.Errorf("%w: %s", sentinel, code)
	}
	return fmt.Errorf("%w: %s", sentinel, message)
}
