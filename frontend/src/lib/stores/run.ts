// Данные одного прогона (кампании или проверки) + его живой прогресс.
// Перечитываем карточку, когда прогон завершился.

import { writable, type Readable } from 'svelte/store';
import { errorMessage } from '#lib/api/client.js';
import { getCampaign } from '#lib/api/campaigns.js';
import { getReview } from '#lib/api/reviews.js';
import type { Campaign, ReviewRun } from '#lib/api/types.js';
import { runProgress, type ProgressStore, type RunKind } from './progress';

export interface RunStore<T> {
	data: Readable<T | null>;
	error: Readable<string | null>;
	loading: Readable<boolean>;
	progress: ProgressStore;
	refresh: () => Promise<void>;
	destroy: () => void;
}

function createRunStore<T>(
	kind: RunKind,
	id: string,
	fetcher: (id: string) => Promise<T>
): RunStore<T> {
	const data = writable<T | null>(null);
	const error = writable<string | null>(null);
	const loading = writable(true);
	const progress = runProgress(kind, id);

	async function refresh(): Promise<void> {
		loading.set(true);
		error.set(null);
		try {
			data.set(await fetcher(id));
		} catch (err) {
			error.set(errorMessage(err));
		} finally {
			loading.set(false);
		}
	}

	void refresh();

	// Прогон завершился — забираем итоговый результат.
	const unsubscribe = progress.terminal.subscribe((done) => {
		if (done) void refresh();
	});

	return {
		data,
		error,
		loading,
		progress,
		refresh,
		destroy: () => {
			unsubscribe();
			progress.stop();
		}
	};
}

export function campaignRun(id: string): RunStore<Campaign> {
	return createRunStore('campaign', id, getCampaign);
}

export function reviewRun(id: string): RunStore<ReviewRun> {
	return createRunStore('review', id, getReview);
}
