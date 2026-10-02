// История кампаний и проверок для сайдбара: единый стор, который обновляется
// при старте, после создания прогона и по завершении прогона.

import { writable } from 'svelte/store';
import { errorMessage } from '#lib/api/client.js';
import { listCampaigns } from '#lib/api/campaigns.js';
import { listReviews } from '#lib/api/reviews.js';
import type { CampaignSummary, ReviewRunSummary } from '#lib/api/types.js';

export const campaigns = writable<CampaignSummary[]>([]);
export const reviews = writable<ReviewRunSummary[]>([]);
export const historyError = writable<string | null>(null);
export const historyLoaded = writable(false);

export async function refreshHistory(): Promise<void> {
	try {
		const [c, r] = await Promise.all([listCampaigns(), listReviews()]);
		campaigns.set(c);
		reviews.set(r);
		historyError.set(null);
	} catch (err) {
		historyError.set(errorMessage(err));
	} finally {
		historyLoaded.set(true);
	}
}
