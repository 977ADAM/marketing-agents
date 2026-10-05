// История кампаний и проверок для сайдбара: единый стор, который обновляется
// при старте, после создания прогона и по завершении прогона.

import { get, writable } from 'svelte/store';
import { errorMessage } from '#lib/api/client.js';
import { listCampaigns } from '#lib/api/campaigns.js';
import { listReviews } from '#lib/api/reviews.js';
import type { CampaignSummary, ReviewRunSummary } from '#lib/api/types.js';

export const campaigns = writable<CampaignSummary[]>([]);
export const reviews = writable<ReviewRunSummary[]>([]);
export const historyError = writable<string | null>(null);
export const historyLoaded = writable(false);

export const campaignCursor = writable<string | undefined>();
export const reviewCursor = writable<string | undefined>();
export const historyLoading = writable(false);
let generation = 0;
let controller: AbortController | undefined;
export async function refreshHistory(): Promise<void> {
	const current = ++generation;
	controller?.abort();
	controller = new AbortController();
	historyLoading.set(true);
	try {
		const [c, r] = await Promise.all([listCampaigns(undefined, controller.signal), listReviews(undefined, controller.signal)]);
		if (current !== generation) return;
		campaignCursor.set(c.nextCursor);
		reviewCursor.set(r.nextCursor);
		campaigns.set(c);
		reviews.set(r);
		historyError.set(null);
	} catch (err) {
		if (current === generation) historyError.set(errorMessage(err));
	} finally {
		if (current === generation) { historyLoaded.set(true); historyLoading.set(false); }
	}
}

export async function loadMoreHistory(kind: 'campaign' | 'review') {
 if (get(historyLoading)) return;
 const cursor = kind === 'campaign' ? campaignCursor : reviewCursor;
 const before = get(cursor);
 if (!before) return;
 const current = generation;
 historyLoading.set(true);
 try {
  const fetcher = kind === 'campaign' ? listCampaigns : listReviews;
  const rows = await fetcher(before, controller?.signal);
  if (current !== generation) return;
  if (kind === 'campaign') campaigns.update(previous => [...new Map([...previous, ...rows as CampaignSummary[]].map(row => [row.id, row])).values()]);
  else reviews.update(previous => [...new Map([...previous, ...rows as ReviewRunSummary[]].map(row => [row.id, row])).values()]);
  cursor.set(rows.nextCursor);
  historyError.set(null);
 } catch (err) { if (current === generation) historyError.set(errorMessage(err)); }
 finally { if (current === generation) historyLoading.set(false); }
}
