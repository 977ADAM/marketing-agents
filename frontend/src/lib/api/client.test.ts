import { afterEach, describe, expect, it, vi } from 'vitest';
import {
	apiUrl,
	ApiError,
	campaignEventsUrl,
	errorMessage,
	postJSON,
	request,
	reviewEventsUrl
} from './client.js';

// fetch подменяем целиком: проверяем и разбор ответа, и то, что уходит наружу.
// Сигнатура мока повторяет fetch, чтобы вызовы записывались с аргументами.
function stubFetch(response: Response) {
	const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => response);
	vi.stubGlobal('fetch', fetchMock);
	return fetchMock;
}

function jsonResponse(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('apiUrl', () => {
	it('строит путь через прокси-роут /api/[...path]', () => {
		expect(apiUrl('campaigns/123')).toBe('/api/campaigns/123');
	});

	it('собирает адреса SSE-потоков кампании и проверки', () => {
		expect(campaignEventsUrl('abc')).toBe('/api/campaigns/abc/events');
		expect(reviewEventsUrl('abc')).toBe('/api/reviews/abc/events');
	});
});

describe('request', () => {
	it('возвращает разобранный JSON', async () => {
		stubFetch(jsonResponse([{ id: '1' }]));
		await expect(request('campaigns')).resolves.toEqual([{ id: '1' }]);
	});

	it('передаёт метод, заголовки и тело запроса', async () => {
		const fetchMock = stubFetch(jsonResponse({ id: '1' }, 202));
		await postJSON('campaigns', { product: 'X' });

		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('/api/campaigns');
		expect(init?.method).toBe('POST');
		expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
		expect(init?.body).toBe(JSON.stringify({ product: 'X' }));
	});

	it('превращает ошибку API в ApiError с кодом из тела', async () => {
		stubFetch(
			jsonResponse({ error: { code: 'validation', message: 'product is required' } }, 400)
		);

		const err = await request('campaigns').catch((e: unknown) => e);
		expect(err).toBeInstanceOf(ApiError);
		expect((err as ApiError).code).toBe('validation');
		expect((err as ApiError).message).toBe('product is required');
		expect((err as ApiError).status).toBe(400);
	});

	it('на не-JSON теле ошибки отдаёт код http_<status> и statusText', async () => {
		stubFetch(new Response('boom', { status: 500, statusText: 'Internal Server Error' }));

		const err = (await request('campaigns').catch((e: unknown) => e)) as ApiError;
		expect(err.code).toBe('http_500');
		expect(err.message).toBe('Internal Server Error');
	});
});

describe('errorMessage', () => {
	it('берёт сообщение ApiError, Error и не падает на неизвестном', () => {
		expect(errorMessage(new ApiError('validation', 'нужен продукт'))).toBe('нужен продукт');
		expect(errorMessage(new Error('сеть'))).toBe('сеть');
		expect(errorMessage('строка')).toBe('Неизвестная ошибка');
	});
});
