package wordstat

// ExtractJSON — шов для тестов разбора SSE-потока MCP: достаёт JSON из кадра.
func ExtractJSON(body []byte) ([]byte, error) { return extractJSON(body) }
