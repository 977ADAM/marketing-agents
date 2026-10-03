// Тесты адресов трассы: лента и деталь события.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { getTrajectory, getTrajectoryEvent } from './trajectory.js';

function stubFetch(body: unknown) {
	const fetchMock = vi.fn(
		async (_url: string, _init?: RequestInit) =>
			new Response(JSON.stringify(body), {
				status: 200,
				headers: { 'Content-Type': 'application/json' }
			})
	);
	vi.stubGlobal('fetch', fetchMock);
	return fetchMock;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('getTrajectory', () => {
	it('берёт ленту кампании по её идентификатору', async () => {
		const fetchMock = stubFetch({ id: 'c1', total: 0, events: [] });
		await expect(getTrajectory('campaign', 'c1')).resolves.toEqual({
			id: 'c1',
			total: 0,
			events: []
		});
		expect(fetchMock.mock.calls[0][0]).toBe('/api/campaigns/c1/trajectory');
	});

	it('берёт ленту проверки текстов по своему префиксу', async () => {
		const fetchMock = stubFetch({ id: 'r1', total: 2, events: [] });
		await getTrajectory('review', 'r1');
		expect(fetchMock.mock.calls[0][0]).toBe('/api/reviews/r1/trajectory');
	});
});

describe('getTrajectoryEvent', () => {
	it('запрашивает событие по номеру и отдаёт тело', async () => {
		const event = { seq: 7, name: 'copywriter', payload: { user: 'бриф' } };
		const fetchMock = stubFetch(event);

		await expect(getTrajectoryEvent('campaign', 'c1', 7)).resolves.toEqual(event);
		expect(fetchMock.mock.calls[0][0]).toBe('/api/campaigns/c1/trajectory/7');
	});
});
