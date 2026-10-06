package traceservice

import trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"

// RecorderImpl открывает тестам пакета trace_test конкретную реализацию
// Recorder: она нужна, чтобы проверить поведение с типизированным nil.
type RecorderImpl = recorder

// TruncateMark — пометка об обрезке payload; тесты сверяют её наличие.
const TruncateMark = trace.TruncatedMark

// ActiveRuns — сколько прогонов держат счётчики в памяти (проверка освобождения).
func ActiveRuns(rec interface{}) int {
	r, ok := rec.(*recorder)
	if !ok {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seq)
}
