// Package corelogger — порт журналирования для ядра.
//
// Ядро (сервисы, раннер) пишет через этот интерфейс и не знает, что под ним:
// конкретный логгер подключается адаптером в composition root — см.
// internal/sloglogger поверх *slog.Logger. Интерфейс намеренно повторяет форму
// slog (сообщение + пары ключ-значение), чтобы адаптер был без потерь, а ядро не
// зависело от конкретной библиотеки.
package corelogger

// Logger — то, что ядру нужно от журнала.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Nop — журнал-заглушка: тесты и случаи, когда лог не нужен.
func Nop() Logger { return nop{} }

type nop struct{}

func (nop) Debug(string, ...any) {}
func (nop) Info(string, ...any)  {}
func (nop) Warn(string, ...any)  {}
func (nop) Error(string, ...any) {}
