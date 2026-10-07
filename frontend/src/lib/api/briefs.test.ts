// Тесты клиента интервью: разбор потока кадров на фейковом fetch и форма
// запроса. Проверяем, что черновик из аргумента уходит в тело: сервер
// состояния не хранит.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamInterview } from './briefs.js';
import type { BriefDraft, InterviewMessage } from './types.js';

const messages: InterviewMessage[] = [{ role: 'user', content: 'Хочу кампанию для термокружки' }];
const draft: BriefDraft = { product: 'Термокружка «Север»', goal: '', audience: '', tone: '' };

// Ответ-поток из готовых кусков: тест сам решает, как кадры нарезаны.
function sseResponse(chunks: string[], init: ResponseInit = {}): Response {
	const encoder = new TextEncoder();
	return new Response(
		new ReadableStream<Uint8Array>({
			start(controller) {
				for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
				controller.close();
			}
		}),
		{ status: 200, headers: { 'Content-Type': 'text/event-stream' }, ...init }
	);
}

function stubFetch(response: Response | Error) {
	const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => {
		if (response instanceof Response) return response;
		throw response;
	});
	vi.stubGlobal('fetch', fetchMock);
	return fetchMock;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('streamInterview', () => {
	it('вызывает onDelta на каждый кадр delta и onDone в конце потока', async () => {
		stubFetch(
			sseResponse([
				'data: {"type":"delta","text":"При"}\n\n',
				'data: {"type":"delta","text":"вет"}\n\ndata: {"type":"done"}\n\n'
			])
		);
		const deltas: string[] = [];
		const onDone = vi.fn();

		await streamInterview(messages, draft, { onDelta: (text) => deltas.push(text), onDone });

		expect(deltas).toEqual(['При', 'вет']);
		expect(onDone).toHaveBeenCalledOnce();
	});

	it('вызывает onBrief с черновиком, пробелами и статусом', async () => {
		const brief: BriefDraft = {
			product: 'Термокружка «Север»',
			goal: 'рост',
			audience: 'велокоммьютеры',
			tone: ''
		};
		stubFetch(
			sseResponse([
				`data: ${JSON.stringify({ type: 'brief', brief, missing: ['tone'], status: 'needs_input' })}\n\n`,
				'data: {"type":"done"}\n\n'
			])
		);
		const onBrief = vi.fn();

		await streamInterview(messages, draft, { onBrief });

		expect(onBrief).toHaveBeenCalledWith(brief, ['tone'], 'needs_input');
	});

	it('битый кадр brief не затирает черновик: без объекта черновика кадр пропускается', async () => {
		stubFetch(
			sseResponse([
				`data: ${JSON.stringify({ type: 'brief', brief: 'мусор', missing: 'нет', status: 42 })}\n\n`,
				'data: {"type":"done"}\n\n'
			])
		);
		const onBrief = vi.fn();

		await streamInterview(messages, draft, { onBrief });

		expect(onBrief).not.toHaveBeenCalled();
	});

	it('отбрасывает нестроковые поля кадра brief так же, как восстановление из хранилища', async () => {
		stubFetch(
			sseResponse([
				`data: ${JSON.stringify({
					type: 'brief',
					brief: { product: 42, goal: 'рост', audience: null, tone: '', region: 7, topics_count: 'много' },
					missing: ['product', 'audience', 'tone'],
					status: 'needs_input'
				})}\n\n`,
				'data: {"type":"done"}\n\n'
			])
		);
		const onBrief = vi.fn();

		await streamInterview(messages, draft, { onBrief });

		expect(onBrief).toHaveBeenCalledWith(
			{ product: '', goal: 'рост', audience: '', tone: '' },
			['product', 'audience', 'tone'],
			'needs_input'
		);
	});

	it('вызывает onError на кадре error', async () => {
		stubFetch(
			sseResponse([
				'data: {"type":"error","message":"не удалось получить ответ интервьюера"}\n\n',
				'data: {"type":"done"}\n\n'
			])
		);
		const onError = vi.fn();

		await streamInterview(messages, draft, { onError });

		expect(onError).toHaveBeenCalledWith('не удалось получить ответ интервьюера');
	});

	it('вызывает onError на сетевой ошибке и не бросает её наружу', async () => {
		stubFetch(new Error('сеть недоступна'));
		const onError = vi.fn();

		await expect(streamInterview(messages, draft, { onError })).resolves.toBeUndefined();
		expect(onError).toHaveBeenCalledWith('сеть недоступна');
	});

	it('молчит при отмене запроса: AbortError — не ошибка хода', async () => {
		stubFetch(new DOMException('отменено', 'AbortError'));
		const onError = vi.fn();

		await streamInterview(messages, draft, { onError });

		expect(onError).not.toHaveBeenCalled();
	});

	it('сообщает об оборванном потоке, если кадра done не было', async () => {
		stubFetch(sseResponse(['data: {"type":"delta","text":"При"}\n\n']));
		const onError = vi.fn();

		await streamInterview(messages, draft, { onError });

		expect(onError).toHaveBeenCalledWith('поток ответа оборван');
	});

	it('вызывает onError с сообщением из конверта API на неуспешном ответе', async () => {		stubFetch(
			new Response(
				JSON.stringify({ error: { code: 'history_too_long', message: 'история диалога слишком длинная' } }),
				{ status: 400, headers: { 'Content-Type': 'application/json' } }
			)
		);
		const onError = vi.fn();
		const onDelta = vi.fn();

		await streamInterview(messages, draft, { onError, onDelta });

		expect(onError).toHaveBeenCalledWith('история диалога слишком длинная');
		expect(onDelta).not.toHaveBeenCalled();
	});

	it('отправляет POST с Accept: text/event-stream и телом {messages, draft}', async () => {
		const fetchMock = stubFetch(sseResponse(['data: {"type":"done"}\n\n']));

		await streamInterview(messages, draft, {});

		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('/api/briefs/interview');
		expect(init?.method).toBe('POST');
		const headers = init?.headers as Record<string, string>;
		expect(headers.Accept).toBe('text/event-stream');
		expect(headers['Content-Type']).toBe('application/json');
		expect(JSON.parse(init?.body as string)).toEqual({ messages, draft });
	});

	it('разбирает кадр, разрезанный между чтениями потока', async () => {
		stubFetch(
			sseResponse(['data: {"type":"del', 'ta","text":"При"}\n\ndata: {"type":"done"}\n\n'])
		);
		const deltas: string[] = [];

		await streamInterview(messages, draft, { onDelta: (text) => deltas.push(text) });

		expect(deltas).toEqual(['При']);
	});
});
