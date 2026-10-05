// Низкоуровневый HTTP-слой: адреса эндпоинтов, разбор ошибок API, JSON-запросы.
// Всё общение с бэкендом идёт через request().

import { resolve } from '$app/paths';

// /api/* — endpoint-роут SvelteKit (src/routes/api/[...path]/+server.ts),
// который проксирует запрос в Go-API. resolve() сам добавляет base приложения
// ('' для standalone, '/marketing' под interpool).
export function apiUrl(path: string): string {
	return resolve('/api/[...path]', { path });
}

export function campaignEventsUrl(id: string): string {
	return apiUrl(`campaigns/${id}/events`);
}

export function reviewEventsUrl(id: string): string {
	return apiUrl(`reviews/${id}/events`);
}

/** Ошибка API: code — машинный код из тела ответа ("validation", "not_found", ...). */
export class ApiError extends Error {
	readonly code: string;
	readonly status: number;

	constructor(code: string, message: string, status = 0) {
		super(message);
		this.code = code;
		this.status = status;
		this.name = 'ApiError';
	}
}

/** Приводит неуспешный ответ к ApiError, разбирая тело {error:{code,message}}. */
async function toApiError(res: Response): Promise<ApiError> {
	let code = `http_${res.status}`;
	let message = res.statusText;
	try {
		const body = (await res.json()) as { error?: { code?: string; message?: string } };
		if (body?.error) {
			code = body.error.code ?? code;
			message = body.error.message ?? message;
		}
	} catch {
		/* тело не JSON — оставляем statusText */
	}
	return new ApiError(code, message || `HTTP ${res.status}`, res.status);
}

/** Запрос к API: путь передаётся уже в виде 'campaigns/123'. */
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(apiUrl(path), init);
	if (!res.ok) throw await toApiError(res);
	return (await res.json()) as T;
}

export function postJSON<T>(path: string, body: unknown, key?:string): Promise<T> {
	return request<T>(path, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json', ...(key?{'Idempotency-Key':key}:{}) },
		body: JSON.stringify(body)
	});
}

/** POST multipart (загрузка .docx). Content-Type выставляет браузер. */
export function postForm<T>(path: string, form: FormData): Promise<T> {
	return request<T>(path, { method: 'POST', body: form });
}

/** Текст ошибки для показа пользователю. */
export function errorMessage(err: unknown): string {
	if (err instanceof ApiError) return err.message;
	if (err instanceof Error) return err.message;
	return 'Неизвестная ошибка';
}
