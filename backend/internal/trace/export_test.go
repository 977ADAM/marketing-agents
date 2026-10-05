package trace

import ()

import ()

// RecorderImpl открывает тестам пакета trace_test конкретную реализацию
// Recorder: она нужна, чтобы проверить поведение с типизированным nil.
type RecorderImpl = recorder

// TruncateMark — пометка об обрезке payload; тесты сверяют её наличие.
const TruncateMark = truncateMark
