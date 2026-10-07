// Клиент интервью по брифу: один POST — один ход диалога, ответ приходит
// потоком SSE. EventSource для POST не годится, поэтому читаем тело ответа
// через fetch + ReadableStream. Сервер состояния не хранит: историю и текущий
// черновик клиент присылает в теле каждого запроса.

import { parseSSE } from '#lib/sse.js';
import { ApiError, apiUrl, errorMessage } from './client.js';
import type { BriefDraft, InterviewFrame, InterviewMessage, InterviewStatus } from './types.js';

export interface InterviewHandlers {
	/** Фрагмент реплики ассистента: приходит по мере генерации. */
	onDelta?: (text: string) => void;
	/** Состояние брифа на этот ход. */
	onBrief?: (brief: BriefDraft, missing: string[], status: InterviewStatus) => void;
	/** Сбой модели, разбора или сети; поток на этом заканчивается. */
	onError?: (message: string) => void;
	/** Конец хода. */
	onDone?: () => void;
}

/** Пустой черновик: четыре обязательных поля, с которых начинается диалог. */
export function emptyBriefDraft(): BriefDraft {
	return { product: '', goal: '', audience: '', tone: '' };
}

/**
 * Терпимо разбирает черновик брифа — одним и тем же способом и для кадра
 * `brief`, и для записи из localStorage. Не-строки и мусор в полях
 * отбрасываются, поэтому в BriefDraft не попадают undefined. null означает, что
 * значение вообще не объект: вызывающий сам решает, чем заменить черновик
 * (кадр без полезной нагрузки прежнее состояние не затирает).
 */
export function readBriefDraft(value: unknown): BriefDraft | null {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) return null;
	const draft = emptyBriefDraft();
	const raw = value as Record<string, unknown>;
	for (const field of ['product', 'goal', 'audience', 'tone'] as const) {
		if (typeof raw[field] === 'string') draft[field] = raw[field];
	}
	if (typeof raw.region === 'string' && raw.region !== '') draft.region = raw.region;
	if (typeof raw.topics_count === 'number' && Number.isFinite(raw.topics_count)) draft.topics_count = raw.topics_count;
	return draft;
}

/**
 * Ведёт один ход интервью. Сетевые и потоковые сбои не бросаются наружу, а
 * уходят в onError: вызывающему (стору) важно показать ошибку в ленте, а не
 * ловить исключение. Отмена по signal ошибкой не считается.
 */
export async function streamInterview(
	messages: InterviewMessage[],
	draft: BriefDraft | undefined,
	handlers: InterviewHandlers,
	signal?: AbortSignal
): Promise<void> {
	let res: Response;
	try {
		res = await fetch(apiUrl('briefs/interview'), {
			method: 'POST',
			headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
			body: JSON.stringify({ messages, draft }),
			signal
		});
	} catch (err) {
		if (!isAbort(err)) handlers.onError?.(errorMessage(err));
		return;
	}
	if (!res.ok) {
		handlers.onError?.(errorMessage(await apiError(res)));
		return;
	}
	if (!res.body) {
		handlers.onError?.('сервер не отдал поток ответа');
		return;
	}

	const reader = res.body.getReader();
	const decoder = new TextDecoder();
	let carry = '';
	// Кадр done закрывает ход: если его не было, поток оборвался на середине.
	let sawDone = false;
	const dispatch: InterviewHandlers = {
		...handlers,
		onDone: () => {
			sawDone = true;
			handlers.onDone?.();
		}
	};
	try {
		for (;;) {
			const { done, value } = await reader.read();
			if (done) break;
			// stream: true — последний байт чанка может обрывать многобайтовый символ.
			const parsed = parseSSE(decoder.decode(value, { stream: true }), carry);
			carry = parsed.carry;
			for (const frame of parsed.frames) handleFrame(frame, dispatch);
		}
		if (!sawDone) handlers.onError?.('поток ответа оборван');
	} catch (err) {
		if (!isAbort(err)) handlers.onError?.(errorMessage(err));
	}
}

/** Разбирает кадр в вызов обработчика; незнакомые и битые кадры молча пропускает. */
function handleFrame(frame: unknown, handlers: InterviewHandlers): void {
	if (typeof frame !== 'object' || frame === null) return;
	const value = frame as Record<string, unknown>;
	const type = value.type as InterviewFrame['type'] | undefined;
	switch (type) {
		case 'delta':
			if (typeof value.text === 'string') handlers.onDelta?.(value.text);
			return;
		case 'brief': {
			// Кадр разбирается так же терпимо, как запись из хранилища: битые поля
			// не должны подменять уже собранный черновик мусором.
			const brief = readBriefDraft(value.brief);
			if (!brief) return; // кадр без объекта черновика: прежний не трогаем
			handlers.onBrief?.(
				brief,
				Array.isArray(value.missing) ? value.missing.filter((key): key is string => typeof key === 'string') : [],
				value.status === 'ready' ? 'ready' : 'needs_input'
			);
			return;
		}
		case 'error':
			handlers.onError?.(typeof value.message === 'string' ? value.message : 'не удалось получить ответ интервьюера');
			return;
		case 'done':
			handlers.onDone?.();
			return;
	}
}

/** Неуспешный ответ: тело {error:{code,message}}, иначе статус ответа. */
async function apiError(res: Response): Promise<ApiError> {
	let code = `http_${res.status}`;
	let message = res.statusText;
	try {
		const body = (await res.json()) as { error?: { code?: string; message?: string } };
		if (body?.error) {
			code = body.error.code ?? code;
			message = body.error.message ?? message;
		}
	} catch {
		/* тело не JSON — остаётся statusText */
	}
	return new ApiError(code, message || `HTTP ${res.status}`, res.status);
}

/** Отмена — не ошибка: пользователь ушёл со страницы или начал новый ход. */
function isAbort(err: unknown): boolean {
	// В браузере DOMException не наследник Error, поэтому смотрим только name.
	return typeof err === 'object' && err !== null && (err as { name?: unknown }).name === 'AbortError';
}
