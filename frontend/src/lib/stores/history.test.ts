import { get } from 'svelte/store';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '#lib/api/client.js';
import type { CampaignSummary, ReviewRunSummary } from '#lib/api/types.js';

vi.mock('#lib/api/campaigns.js', () => ({ getCampaign: vi.fn(), listCampaigns: vi.fn() }));
vi.mock('#lib/api/reviews.js', () => ({ getReview: vi.fn(), listReviews: vi.fn() }));

import { listCampaigns } from '#lib/api/campaigns.js';
import { listReviews } from '#lib/api/reviews.js';
import { campaigns, historyError, historyLoaded, refreshHistory, reviews } from './history.js';

const listCampaignsMock = vi.mocked(listCampaigns);
const listReviewsMock = vi.mocked(listReviews);

const campaignList: CampaignSummary[] = [
	{
		id: 'c1',
		status: 'done',
		brief: { product: 'Эко-бутылка', goal: 'рост', audience: 'ЗОЖ', tone: 'дружелюбный' },
		created_at: '2026-01-01T00:00:00Z'
	}
];

const reviewList: ReviewRunSummary[] = [
	{
		id: 'r1',
		status: 'running',
		brief_text: 'Бриф',
		brief_title: 'Бриф',
		created_at: '2026-01-01T00:00:00Z'
	}
];

beforeEach(() => {
	campaigns.set([]);
	reviews.set([]);
	historyError.set(null);
	historyLoaded.set(false);
	listCampaignsMock.mockReset();
	listReviewsMock.mockReset();
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('refreshHistory', () => {
	it('складывает обе истории, снимает ошибку и отмечает загрузку', async () => {
		listCampaignsMock.mockResolvedValue(campaignList);
		listReviewsMock.mockResolvedValue(reviewList);

		await refreshHistory();

		expect(get(campaigns)).toEqual(campaignList);
		expect(get(reviews)).toEqual(reviewList);
		expect(get(historyError)).toBeNull();
		expect(get(historyLoaded)).toBe(true);
	});

	it('ошибку API показывает в historyError, не роняя сайдбар', async () => {
		listCampaignsMock.mockResolvedValue(campaignList);
		listReviewsMock.mockRejectedValue(new ApiError('internal', 'сервис недоступен', 500));

		await refreshHistory();

		expect(get(historyError)).toBe('сервис недоступен');
		expect(get(historyLoaded)).toBe(true);
	});
});

it('ignores stale history refresh',async()=>{
 let old!: (value:CampaignSummary[])=>void;
 listCampaignsMock.mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockResolvedValueOnce(campaignList);
 listReviewsMock.mockResolvedValue(reviewList);
 const stale=refreshHistory();await refreshHistory();old([]);await stale;expect(get(campaigns)).toEqual(campaignList);
});
