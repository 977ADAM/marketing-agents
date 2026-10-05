import { get } from 'svelte/store';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeEventSource } from '../../test/fake-event-source.js';
import type { Snapshot } from '#lib/api/types.js';
import { runProgress, type RunKind } from './progress.js';

const snapshot: Snapshot = {
	phase: 'producing',
	topics: [],
	topic_total: 2,
	topics_done: 1,
	percent: 52
};

function setup(kind: RunKind = 'campaign', id = 'c1') {
	vi.stubGlobal('EventSource', FakeEventSource);
	const store = runProgress(kind, id);
	return { store, es: FakeEventSource.last };
}

beforeEach(() => {
	FakeEventSource.reset();
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('runProgress', () => {
	it('подключается к SSE-эндпоинту нужного типа прогона', () => {
		expect(setup('campaign', 'c1').es.url).toBe('/api/campaigns/c1/events');
		FakeEventSource.reset();
		expect(setup('review', 'r1').es.url).toBe('/api/reviews/r1/events');
	});

	it('начинает с пустого снимка, без завершения и без обрыва', () => {
		const { store } = setup();
		expect(get(store.snapshot)).toBeNull();
		expect(get(store.terminal)).toBe(false);
		expect(get(store.reconnecting)).toBe(false);
	});

	it('сохраняет снимки из сообщений потока', () => {
		const { store, es } = setup();
		es.emitMessage(JSON.stringify(snapshot));
		expect(get(store.snapshot)).toEqual(snapshot);
	});

	it('игнорирует невалидный JSON, не ломая поток', () => {
		const { store, es } = setup();
		es.emitMessage('{не json');
		expect(get(store.snapshot)).toBeNull();
		es.emitMessage(JSON.stringify(snapshot));
		expect(get(store.snapshot)).toEqual(snapshot);
	});

	it('на событии done фиксирует финальный снимок, завершение и закрывает поток', () => {
		const { store, es } = setup();
		es.emitDone(JSON.stringify({ ...snapshot, phase: 'done', percent: 100 }));
		expect(get(store.snapshot)?.phase).toBe('done');
		expect(get(store.terminal)).toBe(true);
		expect(es.closed).toBe(true);
	});

	it('обрыв соединения включает reconnecting, следующее сообщение выключает', () => {
		const { store, es } = setup();
		es.emitError();
		expect(get(store.reconnecting)).toBe(true);
		es.emitMessage(JSON.stringify(snapshot));
		expect(get(store.reconnecting)).toBe(false);
	});

	it('stop() закрывает поток', () => {
		const { store, es } = setup();
		store.stop();
		expect(es.closed).toBe(true);
	});
});

it('restarts progress after resuming a failed run and ignores the previous stream',()=>{
 const {store,es}=setup();es.emitDone(JSON.stringify({...snapshot,phase:'failed'}));store.restart();const fresh=FakeEventSource.last;expect(fresh).not.toBe(es);expect(get(store.terminal)).toBe(false);es.emitDone(JSON.stringify({...snapshot,phase:'done'}));expect(get(store.terminal)).toBe(false);fresh.emitMessage(JSON.stringify(snapshot));expect(get(store.snapshot)?.phase).toBe('producing');store.stop();
});
