// Разбор потока SSE на кадры. Функция чистая: буфер между чанками передаётся
// снаружи (carry), побочных эффектов и состояния у модуля нет — поэтому логику
// можно проверять юнит-тестами отдельно от сети (как trace-payload.ts).
//
// Формат кадра: строки `data: <payload>`, кадр отделён пустой строкой. Строки
// комментариев (`:`) и прочие поля (`event:`, `id:`) нас не интересуют.

export interface SSEParse {
	/** Разобранные JSON-кадры; битые пропускаются, а не роняют разбор. */
	frames: unknown[];
	/** Незавершённый хвост: его надо отдать в следующий вызов первым аргументом. */
	carry: string;
}

/** Разбирает очередной чанк потока, дополняя им хвост прошлого вызова. */
export function parseSSE(chunk: string, carry: string): SSEParse {
	// Переводы строк нормализуем: кадр может прийти в CRLF-нотации.
	const buffer = (carry + chunk).replace(/\r\n/g, '\n');
	const parts = buffer.split('\n\n');
	// Последний кусок — незавершённый кадр (или пустая строка в конце потока).
	const tail = parts.pop() ?? '';
	const frames: unknown[] = [];
	for (const part of parts) {
		const frame = parseFrame(part);
		if (frame !== undefined) frames.push(frame);
	}
	return { frames, carry: tail };
}

/** Собирает data-строки кадра и разбирает их как JSON; мусор возвращает как undefined. */
function parseFrame(raw: string): unknown {
	const data: string[] = [];
	for (const line of raw.split('\n')) {
		if (!line.startsWith('data:')) continue; // комментарий или другое поле
		// Пробел после двоеточия — часть разделителя, а не данных.
		data.push(line.slice('data:'.length).replace(/^ /, ''));
	}
	if (data.length === 0) return undefined;
	try {
		return JSON.parse(data.join('\n'));
	} catch {
		return undefined; // битый кадр пропускаем: чат из-за него не рвём
	}
}
