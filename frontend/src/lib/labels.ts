// Подписи состояний и вердиктов. Это словарь интерфейса: сопоставление
// «значение из API → текст», без вычислений и порогов.

import type {
	Phase,
	ResearchStage,
	Status,
	TopicSource,
	TopicState,
	TraceKind,
	TraceStatus
} from './api/types';

const STATUS_LABELS: Record<Status, string> = {
	pending: 'В очереди',
	running: 'Генерация',
	done: 'Готово',
	failed: 'Ошибка'
};

/** Подпись статуса; runningLabel уточняет running (например «Проверка»). */
export function statusLabel(status: Status, runningLabel?: string): string {
	return status === 'running' && runningLabel ? runningLabel : STATUS_LABELS[status];
}

export const PHASE_LABELS: Record<Phase, string> = {
 pending: 'Ожидание запуска',
	strategizing: 'Стратегия',
	researching: 'Подбор тем по спросу',
	producing: 'Генерация статей',
	done: 'Готово',
	failed: 'Ошибка'
};

/** Подэтапы подбора тем: показываются, пока идёт фаза researching. */
export const RESEARCH_STAGE_LABELS: Record<ResearchStage, string> = {
	seeds: 'Придумываем поисковые фразы',
	fetching: 'Собираем спрос в Wordstat',
	selecting: 'Отбираем лучшие темы'
};

/** Источник темы: подтверждена спросом или придумана моделью. */
export const SOURCE_LABELS: Record<TopicSource, string> = {
	wordstat: 'по спросу',
	llm: 'гипотеза'
};

/** Подписи состояний сеялок на этапе сбора спроса. */
export const RESEARCH_TOPIC_LABELS: Partial<Record<TopicState, string>> = {
	pending: 'в очереди',
	writing: 'собираем',
	done: 'собрано'
};

export const TOPIC_LABELS: Record<TopicState, string> = {
	pending: 'в очереди',
	writing: 'пишется',
	reviewing: 'на ревью',
	revising: 'доработка',
	done: 'готово'
};

/** Подписи для проверки текстов: те же состояния, другой смысл. */
export const REVIEW_PHASE_LABELS: Partial<Record<Phase, string>> = {
	producing: 'Проверка текстов'
};

export const REVIEW_TOPIC_LABELS: Partial<Record<TopicState, string>> = {
	writing: 'соответствие брифу',
	reviewing: 'корректность текста',
	done: 'проверено'
};

export const ARTICLE_VERDICT_LABELS: Record<string, string> = {
	accept: 'Принято',
	revise: 'На доработку'
};

export const REPORT_VERDICT_LABELS: Record<'pass' | 'fix', string> = {
	pass: '✅ готово к публикации',
	fix: '⚠️ требует доработки'
};

/** Виды событий трассы прогона. */
export const TRACE_KIND_LABELS: Record<TraceKind, string> = {
	llm: 'Модель',
	wordstat: 'Wordstat',
	decision: 'Решение',
	phase: 'Этап',
	result: 'Итог'
};

/** Исход события трассы. */
export const TRACE_STATUS_LABELS: Record<TraceStatus, string> = {
	ok: 'ок',
	error: 'ошибка'
};
