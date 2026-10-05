// Package sloglogger — адаптер: *slog.Logger как corelogger.Logger.
//
// Единственное место, где ядро встречается с конкретной библиотекой логирования.
package sloglogger

import (
	"log/slog"

	corelogger "github.com/977ADAM/marketing-agents/internal/core/logger"
)

// New оборачивает slog-логгер в порт ядра. nil даёт заглушку — как и в трассе,
// логгер не должен быть обязательным.
func New(l *slog.Logger) corelogger.Logger {
	if l == nil {
		return corelogger.Nop()
	}
	return adapter{l: l}
}

type adapter struct{ l *slog.Logger }

func (a adapter) Debug(msg string, args ...any) { a.l.Debug(msg, args...) }
func (a adapter) Info(msg string, args ...any)  { a.l.Info(msg, args...) }
func (a adapter) Warn(msg string, args ...any)  { a.l.Warn(msg, args...) }
func (a adapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }
