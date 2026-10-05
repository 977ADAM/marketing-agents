package wordstat

import ()

// ExtractJSON — шов для тестов разбора SSE-потока MCP: достаёт JSON из кадра.
func ExtractJSON(body []byte) ([]byte, error) { return extractJSON(body) }

// HumanCount — шов для тестов форматирования чисел в трассе (разряды).
func HumanCount(n int64) string { return humanCount(n) }
