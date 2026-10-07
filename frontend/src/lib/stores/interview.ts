// Стор интервью по брифу: лента диалога, собранный черновик и запись состояния
// в localStorage. Это обычные writable-сторы (не руны): файл .ts, а состояние
// нужно и странице, и компонентам — как в stores/history.ts и progress.ts.
//
// Контракт с сервером stateless: каждый ход уходит вместе с текущим черновиком,
// поэтому стор хранит его между запросами и восстанавливает при перезагрузке.

import { derived, get, writable, type Readable, type Writable } from 'svelte/store';
import { streamInterview } from '#lib/api/briefs.js';
import { errorMessage } from '#lib/api/client.js';
import type { Brief, BriefDraft, InterviewMessage, InterviewStatus } from '#lib/api/types.js';

/** Ключ localStorage: лента, черновик и последняя запущенная кампания. */
export const INTERVIEW_KEY = 'interview:v1';

/** Серверный лимит истории: больше 20 реплик эндпоинт не принимает. */
export const MAX_MESSAGES = 20;

/** Признак переросшей истории: запрос не уходит, лента показывает фразу. */
export const OVER_LIMIT_MESSAGE = `история диалога доросла до ${MAX_MESSAGES} сообщений — начните новый диалог`;

/** Пустой черновик: с него начинается диалог и им же заканчивается сброс. */
export function emptyDraft(): BriefDraft {
	return { product: '', goal: '', audience: '', tone: '' };
}

export interface InterviewStore {
	messages: Writable<InterviewMessage[]>;
	draft: Writable<BriefDraft>;
	/** Машинные ключи незаполненных полей из последнего кадра brief. */
	missing: Writable<string[]>;
	/** Статус из последнего кадра brief; до первого хода неизвестен. */
	status: Writable<InterviewStatus | null>;
	streaming: Writable<boolean>;
	error: Writable<string | null>;
	/** id последней запущенной кампании: по нему лента восстанавливает прогон. */
	campaignId: Writable<string | undefined>;
	/** История доросла до лимита: новый ход отправить нельзя. */
	tooLong: Readable<boolean>;
	send(text: string): Promise<void>;
	retry(): Promise<void>;
	reset(): void;
	/** Черновик в форме запуска кампании (Brief для POST /api/campaigns). */
	brief(): Brief;
}

/** То, что лежит в localStorage под ключом interview:v1. */
interface StoredInterview {
	messages: InterviewMessage[];
	draft: BriefDraft;
	campaignId?: string;
}

/** Читает состояние из хранилища, терпимо к битой записи и мусору в полях. */
function readState(storage: Storage | undefined): StoredInterview {
	const empty: StoredInterview = { messages: [], draft: emptyDraft() };
	if (!storage) return empty;
	let raw: string | null = null;
	try {
		raw = storage.getItem(INTERVIEW_KEY);
	} catch {
		return empty; // приватный режим: хранилище недоступно
	}
	if (!raw) return empty;
	try {
		const parsed = JSON.parse(raw) as { messages?: unknown; draft?: unknown; campaignId?: unknown } | null;
		return {
			messages: readMessages(parsed?.messages),
			draft: readDraft(parsed?.draft),
			campaignId: typeof parsed?.campaignId === 'string' ? parsed.campaignId : undefined
		};
	} catch {
		return empty;
	}
}

function readMessages(value: unknown): InterviewMessage[] {
	if (!Array.isArray(value)) return [];
	const messages: InterviewMessage[] = [];
	for (const item of value) {
		if (typeof item !== 'object' || item === null) continue;
		const raw = item as { role?: unknown; content?: unknown };
		if ((raw.role === 'user' || raw.role === 'assistant') && typeof raw.content === 'string') {
			messages.push({ role: raw.role, content: raw.content });
		}
	}
	return messages;
}

function readDraft(value: unknown): BriefDraft {
	const draft = emptyDraft();
	if (typeof value !== 'object' || value === null) return draft;
	const raw = value as Record<string, unknown>;
	for (const field of ['product', 'goal', 'audience', 'tone'] as const) {
		if (typeof raw[field] === 'string') draft[field] = raw[field];
	}
	if (typeof raw.region === 'string' && raw.region !== '') draft.region = raw.region;
	if (typeof raw.topics_count === 'number' && Number.isFinite(raw.topics_count)) draft.topics_count = raw.topics_count;
	return draft;
}

/**
 * Собирает стор интервью. storage передаётся снаружи, чтобы тесты не делили
 * общий localStorage; без хранилища стор просто живёт в памяти.
 */
export function createInterview(storage?: Storage): InterviewStore {
	const restored = readState(storage);
	const messages = writable<InterviewMessage[]>(restored.messages);
	const draft = writable<BriefDraft>(restored.draft);
	const missing = writable<string[]>([]);
	const status = writable<InterviewStatus | null>(null);
	const streaming = writable(false);
	const error = writable<string | null>(null);
	const campaignId = writable<string | undefined>(restored.campaignId);

	let muted = false; // идёт reset: промежуточные состояния писать не нужно
	let generation = 0; // защита от ответа устаревшего хода
	let active: AbortController | undefined;

	/** Пишет состояние в хранилище; при переполнении квоты вытесняет старые реплики. */
	function persist(): void {
		if (muted || !storage) return;
		for (;;) {
			const snapshot: StoredInterview = { messages: get(messages), draft: get(draft) };
			const id = get(campaignId);
			if (id) snapshot.campaignId = id;
			try {
				storage.setItem(INTERVIEW_KEY, JSON.stringify(snapshot));
				return;
			} catch {
				// Квоты не хватило: выбрасываем самую старую реплику и пробуем снова.
				const current = get(messages);
				if (current.length === 0) return; // вытеснять больше нечего
				messages.set(current.slice(1));
			}
		}
	}

	// Лента не пишется на каждую дельту: иначе localStorage получал бы запись на
	// каждый токен. Полное состояние сохраняет runTurn по концу хода.
	messages.subscribe(() => {
		if (!get(streaming)) persist();
	});
	// Черновик и кампания меняются редко — пишем сразу, в том числе когда id
	// кампании кладёт страница: иначе перезагрузка потеряет прогон.
	draft.subscribe(() => persist());
	campaignId.subscribe(() => persist());

	/** Дописывает фрагмент в текущую реплику ассистента. */
	function appendDelta(text: string): void {
		messages.update((list) => {
			const last = list[list.length - 1];
			if (last?.role !== 'assistant') return list;
			const next = list.slice();
			next[next.length - 1] = { ...last, content: last.content + text };
			return next;
		});
	}

	/** Один ход: пустая реплика ассистента, стрим, фиксация результата. */
	async function runTurn(request: InterviewMessage[]): Promise<void> {
		const current = ++generation;
		active?.abort();
		const controller = new AbortController();
		active = controller;
		error.set(null);
		streaming.set(true);
		messages.update((list) => [...list, { role: 'assistant', content: '' }]);
		try {
			await streamInterview(
				request,
				get(draft),
				{
					onDelta: (text) => {
						if (current === generation) appendDelta(text);
					},
					onBrief: (next, missingKeys, nextStatus) => {
						if (current !== generation) return;
						draft.set(next);
						missing.set(missingKeys);
						status.set(nextStatus);
					},
					onError: (message) => {
						if (current === generation) error.set(message);
					}
				},
				controller.signal
			);
		} catch (err) {
			if (current === generation) error.set(errorMessage(err));
		}
		if (current !== generation) return; // ход перебили reset или retry
		streaming.set(false);
		persist();
	}

	async function send(text: string): Promise<void> {
		const content = text.trim();
		if (content === '' || get(streaming)) return;
		if (get(messages).length >= MAX_MESSAGES) {
			error.set(OVER_LIMIT_MESSAGE);
			return;
		}
		const request: InterviewMessage[] = [...get(messages), { role: 'user', content }];
		messages.set(request);
		await runTurn(request);
	}

	async function retry(): Promise<void> {
		if (get(streaming)) return;
		const list = get(messages);
		// Оборванный ход переигрываем: реплику ассистента выбрасываем, серверу
		// уходит та же история, что и в прошлый раз.
		const request = list[list.length - 1]?.role === 'assistant' ? list.slice(0, -1) : list;
		if (request.length === 0) return;
		if (request.length !== list.length) messages.set(request);
		await runTurn(request);
	}

	function reset(): void {
		generation++;
		active?.abort();
		muted = true;
		messages.set([]);
		draft.set(emptyDraft());
		missing.set([]);
		status.set(null);
		error.set(null);
		streaming.set(false);
		campaignId.set(undefined);
		muted = false;
		try {
			storage?.removeItem(INTERVIEW_KEY);
		} catch {
			/* хранилище недоступно — чистить нечего */
		}
	}

	function brief(): Brief {
		const current = get(draft);
		const result: Brief = {
			product: current.product ?? '',
			goal: current.goal ?? '',
			audience: current.audience ?? '',
			tone: current.tone ?? ''
		};
		if (current.region) result.region = current.region;
		if (current.topics_count) result.topics_count = current.topics_count;
		return result;
	}

	return {
		messages,
		draft,
		missing,
		status,
		streaming,
		error,
		campaignId,
		tooLong: derived(messages, (list) => list.length >= MAX_MESSAGES),
		send,
		retry,
		reset,
		brief
	};
}

/** Хранилище браузера; на сервере (SSR) и при запрете доступа — undefined. */
function browserStorage(): Storage | undefined {
	try {
		return typeof window === 'undefined' ? undefined : window.localStorage;
	} catch {
		return undefined;
	}
}

/** Общий стор страницы: один диалог на вкладку. */
export const interview = createInterview(browserStorage());
