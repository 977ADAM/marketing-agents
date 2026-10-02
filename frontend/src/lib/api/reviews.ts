// Запросы к API проверки текстов.

import { postJSON, request } from './client';
import type {
	CreateRunResponse,
	ExtractedDoc,
	ReviewRequest,
	ReviewRun,
	ReviewRunSummary
} from './types';

export function createReview(req: ReviewRequest): Promise<CreateRunResponse> {
	return postJSON<CreateRunResponse>('reviews', req);
}

export function getReview(id: string): Promise<ReviewRun> {
	return request<ReviewRun>(`reviews/${id}`);
}

export function listReviews(): Promise<ReviewRunSummary[]> {
	return request<ReviewRunSummary[]>('reviews');
}

/**
 * Загрузка .docx: API сам разбирает файл и делит текст на заголовок и тело.
 * Тело — сырые байты, а не multipart: так запрос не попадает под CSRF-проверку
 * SvelteKit для form-запросов, а multipart для Go-API собирает фронт-сервер
 * (src/routes/api/[...path]/+server.ts).
 */
export function extractDocx(file: File): Promise<ExtractedDoc> {
	return request<ExtractedDoc>('reviews/extract', {
		method: 'POST',
		headers: {
			'Content-Type': 'application/octet-stream',
			'X-File-Name': encodeURIComponent(file.name)
		},
		body: file
	});
}
