import { get, writable } from 'svelte/store';
import { getTrajectory } from '#lib/api/trajectory.js';
import { errorMessage } from '#lib/api/client.js';
import type { Trajectory } from '#lib/api/types.js';

export function createTrajectoryStore(kind: 'campaign' | 'review', id: string, fetcher = getTrajectory) {
	const data = writable<Trajectory | null>(null);
	const error = writable<string | null>(null);
	const loading = writable(false);
	let timer: ReturnType<typeof setInterval> | undefined;
	let controller: AbortController | undefined;
	let generation = 0;
	let destroyed = false;
	async function refresh() {
		if (destroyed || get(loading)) return;
		const current = ++generation;
		controller = new AbortController();
		loading.set(true);
		try {
			const previous = get(data);
			const after = previous?.next_seq ?? previous?.events.at(-1)?.seq ?? 0;
			const page = await fetcher(kind, id, after, controller.signal);
			if (destroyed || current !== generation) return;
			const events = new Map(previous?.events.map(event => [event.seq, event]));
			for (const event of page.events) events.set(event.seq, event);
			data.set({ ...page, events: [...events.values()].sort((a, b) => a.seq - b.seq) });
			error.set(null);
		} catch (err) {
			if (!destroyed && current === generation) error.set(errorMessage(err));
		} finally {
			if (!destroyed && current === generation) loading.set(false);
		}
	}
	function setActive(active: boolean) {
		if (timer) clearInterval(timer);
		timer = undefined;
		if (active && !destroyed) timer = setInterval(() => void refresh(), 2000);
	}
	function destroy() {
		destroyed = true;
		generation++;
		controller?.abort();
		setActive(false);
	}
	return { data, error, loading, refresh, setActive, destroy };
}
