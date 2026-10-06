// Разбор тела события трассы: сырой payload превращаем в понятные секции —
// размышления модели, промпт, запрос и ответ. Логика вынесена из компонента,
// чтобы её можно было проверить юнит-тестами.
//
// Тело приходит в конверте {data,truncated} (см. рекордер): data — объект, но при
// общей обрезке рекордер отдаёт строку, поэтому обе формы надо пережить.

export type TracePayloadKind = 'sections' | 'json';

export interface TraceMeta {
	model?: string;
	role?: string;
	finishReason?: string;
	reasoningTokens?: number;
}

export interface TraceSection {
	/** Поле тела: reasoning | system | user | response. */
	key: string;
	title: string;
	text: string;
	/** Поле не поместилось в бюджет трассы. */
	truncated: boolean;
	/** Размышления показываем открытыми: за ними и открывают трассу. */
	open: boolean;
}

export interface TraceBody {
	kind: TracePayloadKind;
	sections: TraceSection[];
	/** Pretty-JSON для видов без секций и для неразобранного тела. */
	json: string;
	meta: TraceMeta;
	/** Конверт обрезан целиком: data — строка, на секции не раскладывается. */
	envelopeTruncated: boolean;
}

const SECTION_TITLES: Record<string, string> = {
	reasoning: 'Размышления модели',
	system: 'Системный промпт',
	user: 'Запрос',
	response: 'Ответ'
};

/** Порядок секций: сначала то, как модель думала, потом что ей дали и что она ответила. */
const SECTION_ORDER = ['reasoning', 'system', 'user', 'response'];

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Конверт рекордера: оба поля обязательны, поэтому truncated — надёжный признак. */
function isEnvelope(value: unknown): value is { data: unknown; truncated: boolean } {
	return isRecord(value) && 'data' in value && typeof value.truncated === 'boolean';
}

/** Разворачивает конверт: без него тело отдаётся как есть. */
export function normalizePayload(payload: unknown): { data: unknown; truncated: boolean } {
	if (isEnvelope(payload)) return { data: payload.data, truncated: payload.truncated };
	return { data: payload, truncated: false };
}

function prettyJSON(value: unknown): string {
	if (value === undefined) return '';
	try {
		return JSON.stringify(value, null, 2) ?? String(value);
	} catch {
		return String(value);
	}
}

/** Ответ модели приходит строкой JSON: показываем его разобранным, а не экранированным. */
function renderValue(value: unknown): string {
	if (typeof value !== 'string') return prettyJSON(value);
	const trimmed = value.trim();
	if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return value;
	try {
		return JSON.stringify(JSON.parse(trimmed), null, 2);
	} catch {
		return value; // не JSON — отдаём как есть
	}
}

function readMeta(data: unknown): TraceMeta {
	if (!isRecord(data)) return {};
	const meta: TraceMeta = {};
	if (typeof data.model === 'string') meta.model = data.model;
	if (typeof data.role === 'string') meta.role = data.role;
	if (typeof data.finish_reason === 'string') meta.finishReason = data.finish_reason;
	if (typeof data.reasoning_tokens === 'number') meta.reasoningTokens = data.reasoning_tokens;
	return meta;
}

function buildSections(data: unknown, envelopeTruncated: boolean): TraceSection[] {
	if (!isRecord(data)) return [];
	// Если конверт обрезан целиком, data — строка, и секций не будет.
	const truncatedFields = Array.isArray(data.truncated_fields)
		? data.truncated_fields.filter((f): f is string => typeof f === 'string')
		: [];
	const sections: TraceSection[] = [];
	for (const key of SECTION_ORDER) {
		const value = data[key];
		if (value === undefined || value === null || value === '') continue;
		const text = renderValue(value);
		if (text === '') continue;
		sections.push({
			key,
			title: SECTION_TITLES[key],
			text,
			truncated: truncatedFields.includes(key),
			open: key === 'reasoning' && !envelopeTruncated
		});
	}
	return sections;
}

/** Тело события для вьюера: секции, если это вызов модели, иначе pretty-JSON. */
export function buildTraceBody(payload: unknown): TraceBody {
	const { data, truncated } = normalizePayload(payload);
	if (data === undefined || data === null) {
		return { kind: 'json', sections: [], json: '', meta: {}, envelopeTruncated: truncated };
	}
	const meta = readMeta(data);
	const sections = buildSections(data, truncated);
	if (sections.length > 0) {
		return { kind: 'sections', sections, json: '', meta, envelopeTruncated: truncated };
	}
	const json = typeof data === 'string' ? data : prettyJSON(data);
	return { kind: 'json', sections: [], json, meta, envelopeTruncated: truncated };
}
