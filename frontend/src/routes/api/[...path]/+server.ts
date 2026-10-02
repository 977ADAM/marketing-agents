import type { RequestHandler } from '@sveltejs/kit';
import { BACKEND_URL } from '$app/env/private';

// Хоп-бай-хоп заголовки и длину тела не пересылаем: их выставляет транспорт,
// иначе ломается разбор потокового запроса на той стороне.
const HOP_BY_HOP = new Set([
	'host',
	'connection',
	'keep-alive',
	'proxy-authenticate',
	'proxy-authorization',
	'te',
	'trailer',
	'transfer-encoding',
	'upgrade',
	'content-length'
]);

function forwardHeaders(from: Headers, skip: string[] = []): Headers {
	const out = new Headers();
	for (const [key, value] of from) {
		const name = key.toLowerCase();
		if (HOP_BY_HOP.has(name) || skip.includes(name)) continue;
		out.set(name, value);
	}
	return out;
}

async function forward(target: URL, init: RequestInit): Promise<Response> {
	try {
		const upstream = await fetch(target, init);
		// Тело отдаём потоком: SSE-прогресс нельзя буферизовать.
		return new Response(upstream.body, {
			status: upstream.status,
			headers: upstream.headers
		});
	} catch (err) {
		const message = err instanceof Error ? err.message : String(err);
		return new Response(JSON.stringify({ error: { code: 'bad_gateway', message } }), {
			status: 502,
			headers: { 'Content-Type': 'application/json' }
		});
	}
}

const DOCX_MIME = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';

/**
 * Прокси на Go-API: браузер всегда обращается к тому же origin, а Node-сервер
 * SvelteKit переадресует запрос.
 *
 * Загрузка .docx приходит сырыми байтами (application/octet-stream), а не
 * multipart: SvelteKit блокирует POST с form content-type без совпадающего
 * Origin (CSRF), и вместо отключения защиты multipart для Go-API собираем здесь.
 */
const proxy: RequestHandler = async ({ params, url, request }) => {
	const target = new URL(`/api/${params.path ?? ''}${url.search}`, BACKEND_URL);

	if (request.method === 'POST' && params.path === 'reviews/extract') {
		const bytes = await request.arrayBuffer();
		let filename = 'document.docx';
		const encoded = request.headers.get('x-file-name');
		if (encoded) {
			try {
				filename = decodeURIComponent(encoded);
			} catch {
				/* оставляем имя по умолчанию */
			}
		}
		const form = new FormData();
		form.append('file', new Blob([bytes], { type: DOCX_MIME }), filename);
		return forward(target, {
			method: 'POST',
			headers: forwardHeaders(request.headers, ['content-type']),
			body: form
		});
	}

	const hasBody = request.method !== 'GET' && request.method !== 'HEAD';
	return forward(target, {
		method: request.method,
		headers: forwardHeaders(request.headers),
		body: hasBody ? request.body : undefined,
		// @ts-expect-error duplex обязателен для потокового тела в Node fetch
		duplex: 'half'
	});
};

export const GET = proxy;
export const POST = proxy;
