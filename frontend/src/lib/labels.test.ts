import { describe, expect, it } from 'vitest';
import {
	ARTICLE_VERDICT_LABELS,
	PHASE_LABELS,
	REPORT_VERDICT_LABELS,
	RESEARCH_STAGE_LABELS,
	RESEARCH_TOPIC_LABELS,
	REVIEW_PHASE_LABELS,
	REVIEW_TOPIC_LABELS,
	SOURCE_LABELS,
	statusLabel,
	TRACE_KIND_LABELS,
	TRACE_STATUS_LABELS,
	TOPIC_LABELS
} from './labels.js';

describe('statusLabel', () => {
	it('переводит статусы прогонов', () => {
		expect(statusLabel('pending')).toBe('В очереди');
		expect(statusLabel('running')).toBe('Генерация');
		expect(statusLabel('done')).toBe('Готово');
		expect(statusLabel('failed')).toBe('Ошибка');
	});

	it('уточняет running своим текстом, если он передан', () => {
		expect(statusLabel('running', 'Проверка')).toBe('Проверка');
		expect(statusLabel('done', 'Проверка')).toBe('Готово');
	});
});

describe('словари подписей', () => {
	it('покрывают все фазы и состояния тем', () => {
		expect(Object.keys(PHASE_LABELS).sort()).toEqual(
			['done', 'failed', 'producing', 'researching', 'strategizing'].sort()
		);
		expect(Object.keys(TOPIC_LABELS).sort()).toEqual(
			['done', 'pending', 'reviewing', 'revising', 'writing'].sort()
		);
	});

	it('описывают подбор тем: подэтапы и источник темы', () => {
		expect(Object.keys(RESEARCH_STAGE_LABELS).sort()).toEqual(
			['fetching', 'seeds', 'selecting'].sort()
		);
		expect(RESEARCH_STAGE_LABELS.fetching).toContain('Wordstat');
		expect(SOURCE_LABELS.wordstat).toBe('по спросу');
		expect(SOURCE_LABELS.llm).toBe('гипотеза');
		expect(RESEARCH_TOPIC_LABELS.done).toBe('собрано');
	});

	it('для проверки текстов переопределяют только свои состояния', () => {
		expect(REVIEW_PHASE_LABELS.producing).toBe('Проверка текстов');
		expect(REVIEW_PHASE_LABELS.strategizing).toBeUndefined();
		expect(REVIEW_TOPIC_LABELS.writing).toBe('соответствие брифу');
		expect(REVIEW_TOPIC_LABELS.reviewing).toBe('корректность текста');
	});

	it('содержат вердикты статей и отчётов', () => {
		expect(ARTICLE_VERDICT_LABELS.accept).toBe('Принято');
		expect(ARTICLE_VERDICT_LABELS.revise).toBe('На доработку');
		expect(REPORT_VERDICT_LABELS.pass).toContain('готово к публикации');
		expect(REPORT_VERDICT_LABELS.fix).toContain('требует доработки');
	});
});

describe('словари трассы прогона', () => {
	it('покрывают все виды событий и оба исхода', () => {
		expect(Object.keys(TRACE_KIND_LABELS).sort()).toEqual(
			['decision', 'llm', 'phase', 'result', 'wordstat'].sort()
		);
		expect(Object.keys(TRACE_STATUS_LABELS).sort()).toEqual(['error', 'ok'].sort());
		expect(TRACE_KIND_LABELS.wordstat).toBe('Wordstat');
		expect(TRACE_STATUS_LABELS.error).toBe('ошибка');
	});
});
