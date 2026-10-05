import { get } from 'svelte/store';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeEventSource } from '../../test/fake-event-source.js';
import { ApiError } from '#lib/api/client.js';
import type { Campaign, ReviewRun, Snapshot } from '#lib/api/types.js';

// API-модули подменяем: проверяем поведение стора, а не сеть.
vi.mock('#lib/api/campaigns.js', () => ({ getCampaign: vi.fn(), listCampaigns: vi.fn() }));
vi.mock('#lib/api/reviews.js', () => ({ getReview: vi.fn(), listReviews: vi.fn() }));

import { getCampaign } from '#lib/api/campaigns.js';
import { getReview } from '#lib/api/reviews.js';
import { campaignRun, reviewRun } from './run.js';

const getCampaignMock = vi.mocked(getCampaign);
const getReviewMock = vi.mocked(getReview);

const terminal: Snapshot = {
	phase: 'done',
	topics: [],
	topic_total: 1,
	topics_done: 1,
	percent: 100
};

function campaign(status: 'running' | 'done'): Campaign {
	return {
		id: 'c1',
		client_id: 'default',
		status,
		brief: { product: 'Эко-бутылка', goal: 'рост', audience: 'ЗОЖ', tone: 'дружелюбный' },
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};
}

function review(status: 'running' | 'done'): ReviewRun {
	return {
		id: 'r1',
		client_id: 'default',
		status,
		brief_text: 'Бриф',
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};
}

beforeEach(() => {
	FakeEventSource.reset();
	vi.stubGlobal('EventSource', FakeEventSource);
	getCampaignMock.mockReset();
	getReviewMock.mockReset();
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('campaignRun', () => {
	it('загружает прогон и снимает loading', async () => {
		getCampaignMock.mockResolvedValue(campaign('running'));
		const store = campaignRun('c1');

		await vi.waitFor(() => expect(get(store.loading)).toBe(false));
		expect(get(store.data)?.status).toBe('running');
		expect(get(store.error)).toBeNull();
		expect(getCampaignMock).toHaveBeenCalledWith('c1', expect.any(AbortSignal));
		store.destroy();
	});

	it('по завершении прогона перечитывает результат', async () => {
		getCampaignMock
			.mockResolvedValueOnce(campaign('running'))
			.mockResolvedValueOnce(campaign('done'));
		const store = campaignRun('c1');

		await vi.waitFor(() => expect(get(store.data)?.status).toBe('running'));
		FakeEventSource.last.emitDone(JSON.stringify(terminal));

		await vi.waitFor(() => expect(get(store.data)?.status).toBe('done'));
		expect(getCampaignMock).toHaveBeenCalledTimes(2);
		store.destroy();
	});

	it('ошибку загрузки отдаёт в error', async () => {
		getCampaignMock.mockRejectedValue(new ApiError('not_found', 'campaign not found', 404));
		const store = campaignRun('c1');

		await vi.waitFor(() => expect(get(store.error)).toBe('campaign not found'));
		expect(get(store.loading)).toBe(false);
		expect(get(store.data)).toBeNull();
		store.destroy();
	});

	it('destroy() закрывает поток и не перечитывает данные', async () => {
		getCampaignMock.mockResolvedValue(campaign('running'));
		const store = campaignRun('c1');

		await vi.waitFor(() => expect(get(store.loading)).toBe(false));
		store.destroy();

		expect(FakeEventSource.last.closed).toBe(true);
		FakeEventSource.last.emitDone(JSON.stringify(terminal));
		expect(getCampaignMock).toHaveBeenCalledTimes(1);
	});

	it('привязывает поток и запросы к своему id', async () => {
		getCampaignMock.mockResolvedValue(campaign('running'));
		const first = campaignRun('a');
		const second = campaignRun('b');

		await vi.waitFor(() => expect(getCampaignMock).toHaveBeenCalledTimes(2));
		expect(getCampaignMock.mock.calls.map((call) => call[0])).toEqual(['a', 'b']);
		expect(FakeEventSource.instances.map((es) => es.url)).toEqual([
			'/api/campaigns/a/events',
			'/api/campaigns/b/events'
		]);
		first.destroy();
		second.destroy();
	});
});

describe('reviewRun', () => {
	it('работает через эндпоинты проверки текстов', async () => {
		getReviewMock.mockResolvedValue(review('running'));
		const store = reviewRun('r1');

		await vi.waitFor(() => expect(get(store.data)?.status).toBe('running'));
		expect(getReviewMock).toHaveBeenCalledWith('r1', expect.any(AbortSignal));
		expect(FakeEventSource.last.url).toBe('/api/reviews/r1/events');
		store.destroy();
	});
});

it('latest refresh wins and destroyed store ignores late responses',async()=>{
 let first!: (value:Campaign)=>void;let second!: (value:Campaign)=>void;
 getCampaignMock.mockImplementationOnce(()=>new Promise(resolve=>{first=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{second=resolve}));
 const store=campaignRun('c1');const fresh=store.refresh();second(campaign('done'));await fresh;first(campaign('running'));await Promise.resolve();expect(get(store.data)?.status).toBe('done');store.destroy();
 getCampaignMock.mockImplementationOnce(()=>new Promise(resolve=>{first=resolve}));const gone=campaignRun('c1');gone.destroy();first(campaign('done'));await Promise.resolve();expect(get(gone.data)).toBeNull();
});
