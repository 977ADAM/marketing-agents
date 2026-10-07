// Тесты стора интервью: лента диалога, накопление дельт, черновик между ходами,
// ошибка, сброс, восстановление из localStorage, вытеснение при квоте и лимит
// истории. Каждый тест берёт своё хранилище, общий localStorage не трогаем.

import { get } from 'svelte/store';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Подменяем только сеть: валидаторы черновика (readBriefDraft) остаются настоящими.
vi.mock('#lib/api/briefs.js', async (importOriginal) => {
	const actual = await importOriginal<typeof import('#lib/api/briefs.js')>();
	return { ...actual, streamInterview: vi.fn() };
});

import type { InterviewHandlers } from '#lib/api/briefs.js';
import { streamInterview } from '#lib/api/briefs.js';
import type { BriefDraft, InterviewMessage } from '#lib/api/types.js';
import {
	createInterview,
	emptyDraft,
	INTERVIEW_KEY,
	MAX_MESSAGES,
	STORAGE_WARNING_MESSAGE
} from './interview.js';

const streamMock = vi.mocked(streamInterview);

// Хранилище в памяти с настраиваемой квотой: переполнение бросает, как браузер.
class FakeStorage implements Storage {
	private data = new Map<string, string>();
	quota = Number.POSITIVE_INFINITY;

	get length(): number {
		return this.data.size;
	}
	clear(): void {
		this.data.clear();
	}
	getItem(key: string): string | null {
		return this.data.get(key) ?? null;
	}
	key(index: number): string | null {
		return [...this.data.keys()][index] ?? null;
	}
	removeItem(key: string): void {
		this.data.delete(key);
	}
	setItem(key: string, value: string): void {
		if (value.length > this.quota) throw new Error('QuotaExceededError');
		this.data.set(key, value);
	}
}

// Хранилище, считающее записи: создание стора не должно писать в него вовсе.
class CountingStorage extends FakeStorage {
	writes = 0;

	override setItem(key: string, value: string): void {
		this.writes += 1;
		super.setItem(key, value);
	}
}

/** Один ход стрима: тест сам решает, что «пришло» от сервера. */
function scriptedTurn(script: (handlers: InterviewHandlers) => void): void {
	streamMock.mockImplementationOnce(async (_messages, _draft, handlers) => {
		script(handlers);
	});
}

beforeEach(() => {
	streamMock.mockReset();
});

describe('send', () => {
	it('добавляет реплику пользователя и накапливает дельты в реплику ассистента', async () => {
		const store = createInterview(new FakeStorage());
		scriptedTurn((handlers) => {
			handlers.onDelta?.('При');
			handlers.onDelta?.('вет');
			handlers.onDone?.();
		});

		await store.send('Хочу кампанию');

		expect(get(store.messages)).toEqual([
			{ role: 'user', content: 'Хочу кампанию' },
			{ role: 'assistant', content: 'Привет' }
		]);
		// В запрос уходит история без пустой реплики ассистента текущего хода.
		expect(streamMock.mock.calls[0][0]).toEqual([{ role: 'user', content: 'Хочу кампанию' }]);
		expect(get(store.streaming)).toBe(false);
	});

	it('во время стрима держит признак streaming, а эхо отдаёт через onDone', async () => {
		const store = createInterview(new FakeStorage());
		let seen = false;
		scriptedTurn((handlers) => {
			seen = get(store.streaming);
			handlers.onDone?.();
		});

		await store.send('Привет');

		expect(seen).toBe(true);
		expect(get(store.streaming)).toBe(false);
	});

	it('кадр brief обновляет черновик, пробелы и статус, а следующий запрос несёт этот черновик', async () => {
		const store = createInterview(new FakeStorage());
		const draft: BriefDraft = { product: 'Термокружка', goal: 'рост', audience: 'велокоммьютеры', tone: '' };
		scriptedTurn((handlers) => {
			handlers.onDelta?.('Расскажите про тон');
			handlers.onBrief?.(draft, ['tone'], 'needs_input');
			handlers.onDone?.();
		});

		await store.send('Кампания для термокружки');

		expect(get(store.draft)).toEqual(draft);
		expect(get(store.missing)).toEqual(['tone']);
		expect(get(store.status)).toBe('needs_input');

		// Второй ход: сервер состояния не хранит, поэтому черновик обязан уйти в теле.
		scriptedTurn((handlers) => {
			handlers.onBrief?.({ ...draft, tone: 'дружелюбный' }, [], 'ready');
			handlers.onDone?.();
		});
		await store.send('Тон дружелюбный');

		expect(streamMock.mock.calls[1][1]).toEqual(draft);
		expect(streamMock.mock.calls[1][0]).toEqual([
			{ role: 'user', content: 'Кампания для термокружки' },
			{ role: 'assistant', content: 'Расскажите про тон' },
			{ role: 'user', content: 'Тон дружелюбный' }
		]);
		expect(get(store.draft).tone).toBe('дружелюбный');
		expect(get(store.status)).toBe('ready');
	});

	it('ошибка оставляет накопленный текст и выставляет error', async () => {
		const store = createInterview(new FakeStorage());
		scriptedTurn((handlers) => {
			handlers.onDelta?.('Начало ответа');
			handlers.onError?.('не удалось получить ответ интервьюера');
			handlers.onDone?.();
		});

		await store.send('Привет');

		expect(get(store.messages).at(-1)).toEqual({ role: 'assistant', content: 'Начало ответа' });
		expect(get(store.error)).toBe('не удалось получить ответ интервьюера');
		expect(get(store.streaming)).toBe(false);
	});

	it('пустой текст не отправляет запрос', async () => {
		const store = createInterview(new FakeStorage());

		await store.send('   ');

		expect(streamMock).not.toHaveBeenCalled();
		expect(get(store.messages)).toEqual([]);
	});
});

describe('retry', () => {
	it('переигрывает ход с той же историей, выбрасывая оборванную реплику ассистента', async () => {
		const store = createInterview(new FakeStorage());
		scriptedTurn((handlers) => {
			handlers.onError?.('обрыв потока');
		});
		await store.send('Привет');
		expect(get(store.messages)).toEqual([
			{ role: 'user', content: 'Привет' },
			{ role: 'assistant', content: '' }
		]);

		scriptedTurn((handlers) => {
			handlers.onDelta?.('Ответ');
			handlers.onDone?.();
		});
		await store.retry();

		expect(streamMock.mock.calls[1][0]).toEqual([{ role: 'user', content: 'Привет' }]);
		expect(get(store.messages)).toEqual([
			{ role: 'user', content: 'Привет' },
			{ role: 'assistant', content: 'Ответ' }
		]);
		expect(get(store.error)).toBeNull();
	});
});

describe('reset', () => {
	it('очищает ленту, черновик и запись в localStorage', async () => {
		const storage = new FakeStorage();
		const store = createInterview(storage);
		scriptedTurn((handlers) => {
			handlers.onDelta?.('Ответ');
			handlers.onBrief?.({ product: 'Кружка', goal: '', audience: '', tone: '' }, ['goal', 'audience', 'tone'], 'needs_input');
			handlers.onDone?.();
		});
		await store.send('Привет');
		store.campaignId.set('c1');
		expect(storage.getItem(INTERVIEW_KEY)).not.toBeNull();

		store.reset();

		expect(get(store.messages)).toEqual([]);
		expect(get(store.draft)).toEqual(emptyDraft());
		expect(get(store.missing)).toEqual([]);
		expect(get(store.status)).toBeNull();
		expect(get(store.error)).toBeNull();
		expect(get(store.campaignId)).toBeUndefined();
		expect(storage.getItem(INTERVIEW_KEY)).toBeNull();
	});
});

describe('localStorage', () => {
	it('восстанавливает ленту, черновик и кампанию при создании', () => {
		const storage = new FakeStorage();
		const messages: InterviewMessage[] = [
			{ role: 'user', content: 'Привет' },
			{ role: 'assistant', content: 'Здравствуйте' }
		];
		const draft: BriefDraft = { product: 'Кружка', goal: 'рост', audience: 'ЗОЖ', tone: 'дружелюбный' };
		storage.setItem(INTERVIEW_KEY, JSON.stringify({ messages, draft, campaignId: 'c1' }));

		const store = createInterview(storage);

		expect(get(store.messages)).toEqual(messages);
		expect(get(store.draft)).toEqual(draft);
		expect(get(store.campaignId)).toBe('c1');
	});

	it('переживает битую запись и мусор в полях', () => {
		const storage = new FakeStorage();
		storage.setItem(INTERVIEW_KEY, '{не json');

		const broken = createInterview(storage);
		expect(get(broken.messages)).toEqual([]);
		expect(get(broken.draft)).toEqual(emptyDraft());

		storage.setItem(INTERVIEW_KEY, JSON.stringify({ messages: [{ role: 'system', content: 1 }, 'мусор'], draft: 'нет' }));
		const garbage = createInterview(storage);
		expect(get(garbage.messages)).toEqual([]);
		expect(get(garbage.draft)).toEqual(emptyDraft());
	});

	it('восстанавливает пробелы и статус по черновику: неполный бриф не запустится без предупреждения', () => {
		const storage = new FakeStorage();
		const draft: BriefDraft = { product: 'Кружка', goal: '', audience: 'ЗОЖ', tone: '   ' };
		storage.setItem(INTERVIEW_KEY, JSON.stringify({ messages: [{ role: 'user', content: 'Привет' }], draft }));

		const store = createInterview(storage);

		// Те же четыре обязательных поля и то же правило «пробельное — пустое», что на сервере.
		expect(get(store.missing)).toEqual(['goal', 'tone']);
		expect(get(store.status)).toBe('needs_input');
	});

	it('восстанавливает готовый черновик как ready без пробелов', () => {
		const storage = new FakeStorage();
		const draft: BriefDraft = { product: 'Кружка', goal: 'рост', audience: 'ЗОЖ', tone: 'дружелюбный' };
		storage.setItem(INTERVIEW_KEY, JSON.stringify({ messages: [{ role: 'user', content: 'Привет' }], draft }));

		const store = createInterview(storage);

		expect(get(store.missing)).toEqual([]);
		expect(get(store.status)).toBe('ready');
	});

	it('без записи в хранилище статус ещё неизвестен, а пробелов не показывает', () => {
		const store = createInterview(new FakeStorage());

		expect(get(store.missing)).toEqual([]);
		expect(get(store.status)).toBeNull();
	});

	it('восстанавливает ошибку последнего хода, чтобы после перезагрузки было что повторить', () => {
		const storage = new FakeStorage();
		storage.setItem(
			INTERVIEW_KEY,
			JSON.stringify({
				messages: [
					{ role: 'user', content: 'Привет' },
					{ role: 'assistant', content: '' }
				],
				draft: emptyDraft(),
				error: 'поток ответа оборван'
			})
		);

		const store = createInterview(storage);

		expect(get(store.error)).toBe('поток ответа оборван');
	});

	it('сохраняет ошибку хода в хранилище вместе с лентой', async () => {
		const storage = new FakeStorage();
		const store = createInterview(storage);
		scriptedTurn((handlers) => {
			handlers.onError?.('обрыв потока');
		});

		await store.send('Привет');

		const stored = JSON.parse(storage.getItem(INTERVIEW_KEY) as string) as { error?: string };
		expect(stored.error).toBe('обрыв потока');
	});

	it('при переполнении квоты вытесняет самые старые реплики и дописывает остаток', () => {
		const storage = new FakeStorage();
		const store = createInterview(storage);
		const messages: InterviewMessage[] = [1, 2, 3, 4, 5].map((i) => ({ role: 'user', content: `реплика ${i}` }));
		// Квоты хватает ровно на две реплики: запись пяти не пройдёт.
		storage.quota = JSON.stringify({ messages: messages.slice(3), draft: emptyDraft() }).length;

		store.messages.set(messages);

		const stored = JSON.parse(storage.getItem(INTERVIEW_KEY) as string) as { messages: InterviewMessage[] };
		expect(stored.messages).toEqual(messages.slice(3));
		expect(get(store.messages)).toEqual(messages.slice(3));
	});

	it('не вытесняет текущий ход, если в хранилище не помещается даже он', () => {
		const storage = new FakeStorage();
		const store = createInterview(storage);
		const messages: InterviewMessage[] = [1, 2, 3, 4, 5].map((i) => ({ role: 'user', content: `реплика ${i}` }));
		storage.quota = 0; // не помещается ничего

		store.messages.set(messages);

		// Последняя реплика (видимая правда) на месте, запись не удалась — говорим об этом.
		expect(get(store.messages)).toEqual(messages);
		expect(storage.getItem(INTERVIEW_KEY)).toBeNull();
		expect(get(store.storageWarning)).toBe(STORAGE_WARNING_MESSAGE);
	});

	it('во время хода не вытесняет реплику ассистента из-за квоты: дельты продолжают копиться', async () => {
		const storage = new FakeStorage();
		const store = createInterview(storage);
		storage.quota = 0; // ни одна запись не проходит
		scriptedTurn((handlers) => {
			// Кадр brief обновляет черновик прямо посреди хода — и раньше именно
			// здесь вытеснялся пустой placeholder, после чего дельты пропадали.
			handlers.onBrief?.({ product: 'Кружка', goal: '', audience: '', tone: '' }, ['goal', 'audience', 'tone'], 'needs_input');
			handlers.onDelta?.('Ответ');
			handlers.onDone?.();
		});

		await store.send('Привет');

		expect(get(store.messages)).toEqual([
			{ role: 'user', content: 'Привет' },
			{ role: 'assistant', content: 'Ответ' }
		]);
		expect(get(store.storageWarning)).toBe(STORAGE_WARNING_MESSAGE);
	});

	it('при создании стора не пишет в хранилище и не срезает восстановленную ленту', () => {
		const storage = new CountingStorage();
		const messages: InterviewMessage[] = [1, 2, 3, 4, 5].map((i) => ({ role: 'user', content: `реплика ${i}` }));
		const draft: BriefDraft = { product: 'Кружка', goal: 'рост', audience: 'ЗОЖ', tone: 'дружелюбный' };
		storage.setItem(INTERVIEW_KEY, JSON.stringify({ messages, draft }));
		// Квоты хватило бы только на хвост из двух реплик: запись при создании
		// срезала бы восстановленную историю до первого показа.
		storage.quota = JSON.stringify({ messages: messages.slice(3), draft }).length;
		storage.writes = 0;

		const store = createInterview(storage);

		expect(storage.writes).toBe(0);
		expect(get(store.messages)).toEqual(messages);
		expect(JSON.parse(storage.getItem(INTERVIEW_KEY) as string)).toEqual({ messages, draft });
	});
});

describe('лимит истории', () => {
	it('после 20 сообщений не отправляет запрос и выставляет признак переросшей истории', async () => {
		const store = createInterview(new FakeStorage());
		store.messages.set(
			Array.from({ length: MAX_MESSAGES }, (_value, index) => ({
				role: index % 2 === 0 ? ('user' as const) : ('assistant' as const),
				content: `реплика ${index}`
			}))
		);

		await store.send('ещё одна');

		expect(streamMock).not.toHaveBeenCalled();
		expect(get(store.error)).toContain(String(MAX_MESSAGES));
		expect(get(store.tooLong)).toBe(true);
		expect(get(store.messages)).toHaveLength(MAX_MESSAGES);
	});
});

describe('brief', () => {
	it('отдаёт черновик в форме запуска кампании вместе с регионом и числом тем', async () => {
		const store = createInterview(new FakeStorage());
		scriptedTurn((handlers) => {
			handlers.onBrief?.(
				{ product: 'Кружка', goal: 'рост', audience: 'ЗОЖ', tone: 'дружелюбный', region: '213', topics_count: 3 },
				[],
				'ready'
			);
			handlers.onDone?.();
		});
		await store.send('Всё есть');

		expect(store.brief()).toEqual({
			product: 'Кружка',
			goal: 'рост',
			audience: 'ЗОЖ',
			tone: 'дружелюбный',
			region: '213',
			topics_count: 3
		});
	});
});
