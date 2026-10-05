// Живой прогресс прогона по SSE. Данные приходят готовыми снимками от API —
// стор только подписывается на поток и отписывается.

import { writable, type Readable } from 'svelte/store';
import { campaignEventsUrl, reviewEventsUrl } from '#lib/api/client.js';
import type { Snapshot } from '#lib/api/types.js';

export type RunKind = 'campaign' | 'review';

export interface ProgressStore {
	snapshot: Readable<Snapshot | null>;
	/** Прогон завершён (событие done) — можно дочитывать результат. */
	terminal: Readable<boolean>;
	/** Соединение потеряно, EventSource переподключается сам. */
	reconnecting: Readable<boolean>;
	stop: () => void;
 restart:()=>void;
}

export function runProgress(kind: RunKind, id: string): ProgressStore {
	const snapshot = writable<Snapshot | null>(null);
	const terminal = writable(false);
	const reconnecting = writable(false);

	const url = kind === 'campaign' ? campaignEventsUrl(id) : reviewEventsUrl(id);
	let es:EventSource;let generation=0;

	const parse = (data: string): Snapshot | null => {
		try {
			return JSON.parse(data) as Snapshot;
		} catch {
			return null;
		}
	};

 function connect(){const current=++generation;es=new EventSource(url);
	es.onmessage = (e) => {
 if (current!==generation)return;
		reconnecting.set(false);
		const next = parse(e.data);
		if (next) snapshot.set(next);
	};

	es.addEventListener('done', (e) => {
 if (current!==generation)return;
		const next = parse((e as MessageEvent<string>).data);
		if (next) snapshot.set(next);
		terminal.set(true);
		es.close(); // прогон завершён — гасим авто-реконнект
	});

	es.onerror = () => {if(current===generation)reconnecting.set(true);};

 }
 connect();
 function stop(){generation++;es.close();}
 function restart(){stop();snapshot.set(null);terminal.set(false);reconnecting.set(false);connect();}

	return { snapshot, terminal, reconnecting, stop,restart };
}
