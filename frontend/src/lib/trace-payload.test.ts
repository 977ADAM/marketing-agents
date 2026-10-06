import { describe, expect, it } from 'vitest';
import { buildTraceBody, normalizePayload } from './trace-payload.js';

describe('normalizePayload', () => {
	it('разворачивает конверт рекордера', () => {
		expect(normalizePayload({ data: { a: 1 }, truncated: true })).toEqual({ data: { a: 1 }, truncated: true });
	});

	it('тело без конверта отдаёт как есть', () => {
		expect(normalizePayload({ topic: 'шины' })).toEqual({ data: { topic: 'шины' }, truncated: false });
	});

	it('объект с полем data, но без признака обрезки конвертом не считается', () => {
		expect(normalizePayload({ data: 'x', other: 1 })).toEqual({ data: { data: 'x', other: 1 }, truncated: false });
	});
});

describe('buildTraceBody', () => {
	it('собирает секции вызова модели: размышления, промпты, ответ', () => {
		const body = buildTraceBody({
			data: {
				model: 'deepseek-v4-pro',
				role: 'critic',
				finish_reason: 'stop',
				reasoning_tokens: 120,
				system: 'Ты критик',
				user: 'Оцени статью',
				reasoning: 'Сначала посмотрю на структуру',
				response: '{"score":7}',
				truncated_fields: ['user']
			},
			truncated: false
		});

		expect(body.kind).toBe('sections');
		expect(body.meta).toEqual({
			model: 'deepseek-v4-pro',
			role: 'critic',
			finishReason: 'stop',
			reasoningTokens: 120
		});
		// Размышления идут первыми и раскрыты: за ними и открывают трассу.
		expect(body.sections.map(section => section.key)).toEqual(['reasoning', 'system', 'user', 'response']);
		expect(body.sections[0].open).toBe(true);
		expect(body.sections[1].open).toBe(false);
		expect(body.sections.find(section => section.key === 'user')?.truncated).toBe(true);
		expect(body.sections.find(section => section.key === 'system')?.truncated).toBe(false);
	});

	it('ответ-строку JSON показывает разобранным', () => {
		const body = buildTraceBody({ data: { response: '{"score":7,"issues":[]}' }, truncated: false });
		expect(body.sections[0].text).toBe('{\n  "score": 7,\n  "issues": []\n}');
	});

	it('не-JSON ответ оставляет текстом', () => {
		const body = buildTraceBody({ data: { response: 'извините, не могу' }, truncated: false });
		expect(body.sections[0].text).toBe('извините, не могу');
	});

	it('обрезанный целиком конверт отдаёт строку как есть', () => {
		const body = buildTraceBody({ data: '{"system":"очень длинный промпт', truncated: true });
		expect(body.kind).toBe('json');
		expect(body.envelopeTruncated).toBe(true);
		expect(body.json).toBe('{"system":"очень длинный промпт');
	});

	it('тело решения без секций показывается pretty-JSON', () => {
		const body = buildTraceBody({ topic: 'шины', selected: true });
		expect(body.kind).toBe('json');
		expect(body.json).toContain('"selected": true');
		expect(body.sections).toHaveLength(0);
	});

	it('пустое тело не ломает разбор', () => {
		const body = buildTraceBody(undefined);
		expect(body.kind).toBe('json');
		expect(body.json).toBe('');
		expect(body.sections).toHaveLength(0);
	});
});
