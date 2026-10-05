// Запросы к API трассы прогона: журнал того, что делали агенты.

import { request } from './client';
import type { Trajectory, TrajectoryEvent } from './types';

// Префикс вида прогона: у кампаний и проверок текстов свои эндпоинты.
function base(kind: 'campaign' | 'review'): string {
	return kind === 'campaign' ? 'campaigns' : 'reviews';
}

/** Лента событий прогона: без тел, но с признаком has_payload у каждого события. */
export function getTrajectory(kind: 'campaign' | 'review', id: string, after = 0, signal?: AbortSignal): Promise<Trajectory> {
	return request<Trajectory>(`${base(kind)}/${id}/trajectory${after > 0 ? `?after_seq=${after}` : ''}`, { signal });
}

/**
 * Одно событие вместе с телом. Отдельным запросом, потому что тела бывают
 * крупными (промпт и ответ статьи), и тащить их в ленту нельзя.
 */
export function getTrajectoryEvent(
	kind: 'campaign' | 'review',
	id: string,
	seq: number
): Promise<TrajectoryEvent> {
	return request<TrajectoryEvent>(`${base(kind)}/${id}/trajectory/${seq}`);
}
