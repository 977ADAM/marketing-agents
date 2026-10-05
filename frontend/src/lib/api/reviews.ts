// Запросы к API проверки текстов.

import { creationRequest } from './create.js';
import { postJSON, request, historyPage, type HistoryRows } from './client';
import type {
	CreateRunResponse,
	ExtractedDoc,
	ReviewRequest,
	ReviewRun,
	ReviewRunSummary
} from './types';

const create=creationRequest((path,body,key)=>postJSON<CreateRunResponse>(path,body,key));

export function createReview(req: ReviewRequest): Promise<CreateRunResponse> {
	return create('reviews', req);
}

export function getReview(id: string, signal?: AbortSignal): Promise<ReviewRun> {
	return request<ReviewRun>(`reviews/${id}`, { signal });
}

export function listReviews(before?: string, signal?: AbortSignal): Promise<HistoryRows<ReviewRunSummary>> {
	return historyPage<ReviewRunSummary>('reviews', before, signal);
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

export function retryReview(id:string):Promise<CreateRunResponse>{return postJSON<CreateRunResponse>(`reviews/${id}/retry`,{});}
