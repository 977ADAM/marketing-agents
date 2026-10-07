// Стор интервью по брифу: лента диалога, собранный черновик и запись состояния
// в localStorage. Это обычные writable-сторы (не руны): файл .ts, а состояние
// нужно и странице, и компонентам — как в stores/history.ts и progress.ts.
//
// Контракт с сервером stateless: каждый ход уходит вместе с текущим черновиком,
// поэтому стор хранит его между запросами и восстанавливает при перезагрузке.

import { derived, get, writable, type Readable, type Writable } from 'svelte/store';
import { emptyBriefDraft, readBriefDraft, streamInterview } from '#lib/api/briefs.js';
import { errorMessage } from '#lib/api/client.js';
import type { Brief, BriefDraft, InterviewMessage, InterviewStatus } from '#lib/api/types.js';

/** Ключ localStorage: лента, черновик и последняя запущенная кампания. */
export const INTERVIEW_KEY = 'interview:v1';

/** Серверный лимит истории: больше 20 реплик эндпоинт не принимает. */
export const MAX_MESSAGES = 20;

/** Признак переросшей истории: запрос не уходит, лента показывает фразу. */
export const OVER_LIMIT_MESSAGE = `история диалога доросла до ${MAX_MESSAGES} сообщений — начните новый диалог`;

/** Хранилище не приняло запись: история может пропасть после перезагрузки. */
export const STORAGE_WARNING_MESSAGE =
	'не удалось сохранить историю: в хранилище не хватает места — после перезагрузки она может пропасть';

/** Обязательные поля брифа: тот же список и порядок, что на сервере. */
const REQUIRED_FIELDS = ['product', 'goal', 'audience', 'tone'] as const;

/** Пустой черновик: с него начинается диалог и им же заканчивается сброс. */
export function emptyDraft(): BriefDraft {
	return emptyBriefDraft();
}

/**
 * Пробелы черновика по обязательным полям. То же правило, что у сервиса
 * (`strings.TrimSpace(s) != ""`), — иначе после перезагрузки интерфейс показал бы
 * не тот статус, что сервер, и запуск прошёл бы без предупреждения.
 */
function missingFields(draft: BriefDraft): string[] {
	return REQUIRED_FIELDS.filter((field) => (draft[field] ?? '').trim() === '');
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
	/** Хранилище не приняло запись: часть ленты может пропасть после перезагрузки. */
	storageWarning: Writable<string | null>;
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
	/** Ошибка последнего хода: по ней после перезагрузки видна кнопка «Повторить». */
	error?: string;
}

/**
 * Читает состояние из хранилища, терпимо к битой записи и мусору в полях.
 * undefined — записи нет (или она битая): тогда состояние ещё неизвестно и его
 * нельзя выводить из пустого черновика.
 */
function readState(storage: Storage | undefined): StoredInterview | undefined {
	if (!storage) return undefined;
	let raw: string | null = null;
	try {
		raw = storage.getItem(INTERVIEW_KEY);
	} catch {
		return undefined; // приватный режим: хранилище недоступно
	}
	if (!raw) return undefined;
	try {
		const parsed = JSON.parse(raw) as
			| { messages?: unknown; draft?: unknown; campaignId?: unknown; error?: unknown }
			| null;
		if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return undefined;
		return {
			messages: readMessages(parsed.messages),
			draft: readDraft(parsed.draft),
			campaignId: typeof parsed.campaignId === 'string' ? parsed.campaignId : undefined,
			error: typeof parsed.error === 'string' && parsed.error !== '' ? parsed.error : undefined
		};
	} catch {
		return undefined;
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
	return readBriefDraft(value) ?? emptyDraft();
}

/**
 * Собирает стор интервью. storage передаётся снаружи, чтобы тесты не делили
 * общий localStorage; без хранилища стор просто живёт в памяти.
 */
export function createInterview(storage?: Storage): InterviewStore {
	const restored = readState(storage);
	// Пробелы и статус восстанавливаем по черновику: сервер их не пересчитает,
	// пока не пройдёт следующий ход, а запуск кампании должен предупреждать о
	// пробелах сразу после перезагрузки. Без записи в хранилище статус неизвестен.
	const restoredMissing = restored ? missingFields(restored.draft) : [];
	const messages = writable<InterviewMessage[]>(restored?.messages ?? []);
	const draft = writable<BriefDraft>(restored?.draft ?? emptyDraft());
	const missing = writable<string[]>(restoredMissing);
	const status = writable<InterviewStatus | null>(
		restored ? (restoredMissing.length === 0 ? 'ready' : 'needs_input') : null
	);
	const streaming = writable(false);
	const error = writable<string | null>(restored?.error ?? null);
	const campaignId = writable<string | undefined>(restored?.campaignId);
	const storageWarning = writable<string | null>(null);

	let muted = false; // идёт reset: промежуточные состояния писать не нужно
	let writing = false; // идёт запись: messages.set внутри persist не должен её зациклить
	let generation = 0; // защита от ответа устаревшего хода
	let active: AbortController | undefined;

	/**
	 * Сколько реплик с конца обязано остаться при вытеснении: текущий ход целиком
	 * (последняя реплика пользователя и всё, что после неё), иначе — последняя
	 * реплика. Ниже этой границы лента опустеть не может.
	 */
	function turnFloor(list: InterviewMessage[]): number {
		for (let i = list.length - 1; i >= 0; i--) {
			if (list[i].role === 'user') return list.length - i;
		}
		return list.length === 0 ? 0 : 1;
	}

	/**
	 * Пишет состояние в хранилище. При переполнении квоты вытесняет самые старые
	 * реплики, но не ниже текущего хода: иначе квота срезала бы только что
	 * законченный вопрос с ответом или пустую реплику посреди хода, и все
	 * следующие дельты пропали бы. Если не помещается даже текущий ход, стор не
	 * трогаем (видимая лента остаётся целой) и показываем предупреждение.
	 */
	function persist(): void {
		if (muted || writing || !storage) return;
		writing = true;
		try {
			const visible = get(messages);
			const floor = turnFloor(visible);
			let keep = visible.length;
			for (;;) {
				const kept = keep === visible.length ? visible : visible.slice(visible.length - keep);
				const snapshot: StoredInterview = { messages: kept, draft: get(draft) };
				const id = get(campaignId);
				if (id) snapshot.campaignId = id;
				const lastError = get(error);
				if (lastError) snapshot.error = lastError;
				try {
					storage.setItem(INTERVIEW_KEY, JSON.stringify(snapshot));
					// Вытеснение переносим в стор только после удачной записи: иначе
					// неудачная запись оставила бы ленту пустее, чем она была.
					if (keep !== visible.length) messages.set(kept);
					storageWarning.set(null);
					return;
				} catch {
					if (keep <= floor) {
						storageWarning.set(STORAGE_WARNING_MESSAGE);
						return;
					}
					keep -= 1;
				}
			}
		} finally {
			writing = false;
		}
	}

	// Стор только что создан: subscribe синхронно отдаёт текущее значение, и
	// запись на этом «первом» кадре была бы записью на импорте (для синглтона) —
	// при почти полной квоте она срезала бы восстановленную ленту до первого
	// показа. Поэтому первый (конструкторский) кадр каждой подписки пропускаем.
	let ready = false;
	// Лента не пишется на каждую дельту: иначе localStorage получал бы запись на
	// каждый токен. Полное состояние сохраняет runTurn по концу хода.
	messages.subscribe(() => {
		if (ready && !get(streaming)) persist();
	});
	// Черновик и кампания меняются редко — пишем сразу, в том числе когда id
	// кампании кладёт страница: иначе перезагрузка потеряет прогон.
	draft.subscribe(() => {
		if (ready) persist();
	});
	campaignId.subscribe(() => {
		if (ready) persist();
	});
	ready = true;

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
		storageWarning.set(null);
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
		storageWarning,
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
