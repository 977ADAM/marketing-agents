// Типы, которыми обменивается фронт и API. Один источник правды по контракту:
// здесь только wire-формат (snake_case как в JSON), без UI-состояний.

export type Status = 'pending' | 'running' | 'done' | 'failed';

/** Градация оценки: считает бэкенд, фронт только красит по ней бейдж. */
export type Severity = 'good' | 'warn' | 'bad';

// --- кампании ---

export interface Brief {
	product: string;
	goal: string;
	audience: string;
	tone: string;
	/** geo ID Яндекса для подбора тем: 225 — Россия, 213 — Москва. */
	region?: string;
	/** Сколько статей нужно по медиаплану; идей подбираем вдвое больше. */
	topics_count?: number;
}

export interface Topic {
	title: string;
	angle: string;
	points: string[];
}

export interface Strategy {
 warnings?:string[];
	positioning: string;
	topics: Topic[];
	/** Все рассмотренные темы (вдвое больше, чем статей) с доказательствами. */
	topic_candidates?: TopicCandidate[];
	/** Сколько обращений к Wordstat потребовал подбор тем. */
	wordstat_calls?: number;
}

// --- подбор тем по поисковому спросу ---

export type TopicSource = 'wordstat' | 'llm';

/** Подэтап фазы researching. */
export type ResearchStage = 'seeds' | 'fetching' | 'selecting';

export interface PhraseCount {
	phrase: string;
	count: number;
}

/** Сезонная поправка: окно Wordstat — 30 дней, у сезонных тем спрос скачет. */
export interface Seasonality {
	peak: number;
	peak_month?: string;
	trough: number;
	ratio: number;
	seasonal: boolean;
}

export interface RegionShare {
	region_id: string;
	name?: string;
	count: number;
	share: number;
	affinity_index: number;
}

/**
 * Тема, рассмотренная при подборе. Volume — максимальная частотность среди
 * цитат (нижняя оценка спроса), а не сумма формулировок: популярные запросы
 * являются подмножествами широкой частотности. У тем source='llm' цифр нет.
 */
export interface TopicCandidate {
	id: string;
	title: string;
	goal: string;
	task: string;
	source: TopicSource;
	selected: boolean;
	volume: number;
	head: string;
	queries: PhraseCount[];
	season?: Seasonality;
	regions?: RegionShare[];
	intent?: string;
	/** Почему тема не пошла в генерацию (пусто — прошла отбор). */
	reject?: string;
}

// issues опционален: Go сериализует nil-срез как null, не как [].
export interface Review {
	score: number;
	issues?: string[];
	verdict: string;
	severity: Severity;
}

export interface Deliverable {
	topic: string;
	title: string;
	body: string;
	cta: string;
	review: Review | null;
}

export interface Campaign {
 resume_available?:boolean;
	id: string;
	client_id: string;
	status: Status;
	brief: Brief;
	strategy?: Strategy;
	deliverables?: Deliverable[];
	progress?: Snapshot;
	cost_usd?: number;
 cost_known?:boolean;
	error?: string;
	created_at: string;
	updated_at: string;
}

export interface CampaignSummary {
	id: string;
	status: Status;
	brief: Brief;
	cost_usd?: number;
 cost_known?:boolean;
	created_at: string;
}

// --- проверка готовых текстов ---

export interface ReviewText {
	title: string;
	body: string;
}

export interface ReviewRequest {
	brief: string;
	texts: ReviewText[];
}

export interface CheckScore {
	score: number;
	issues?: string[];
	severity: Severity;
}

export interface TextReport {
	title: string;
	compliance: CheckScore;
	quality: CheckScore;
	overall: number;
	verdict: 'pass' | 'fix';
	severity: Severity;
}

export interface ReviewResult {
	items: TextReport[];
	/** Сколько текстов прошло проверку — сводку считает API. */
	passed: number;
	cost_usd: number;
}

export interface ReviewRun {
 resume_available?:boolean;
	id: string;
	client_id: string;
	status: Status;
	brief_text: string;
	result?: ReviewResult;
	progress?: Snapshot;
	cost_usd?: number;
 cost_known?:boolean;
	error?: string;
	created_at: string;
	updated_at: string;
}

export interface ReviewRunSummary {
	id: string;
	status: Status;
	brief_text: string;
	/** Первая строка брифа — заголовок для списка, готовит API. */
	brief_title?: string;
	cost_usd?: number;
 cost_known?:boolean;
	created_at: string;
}

/** Результат разбора .docx: API уже разделил текст на заголовок и тело. */
export interface ExtractedDoc {
	title: string;
	body: string;
	text: string;
}

// --- общее ---

export interface CreateRunResponse {
	id: string;
	status: Status;
}

// --- прогресс прогона (SSE) ---

export type Phase =
 | 'pending' | 'strategizing' | 'researching' | 'producing' | 'done' | 'failed';
export type TopicState = 'pending' | 'writing' | 'reviewing' | 'revising' | 'done';

export interface TopicProgress {
	index: number;
	title: string;
	state: TopicState;
	iter?: number;
	score?: number;
}

export interface Snapshot {
	phase: Phase;
	topics: TopicProgress[];
	topic_total: number;
	topics_done: number;
	percent: number;
	/** Подэтап фазы researching (приходит только на этапе подбора тем). */
	stage?: ResearchStage;
}

// --- трасса прогона ---

export type TraceKind = 'llm' | 'wordstat' | 'decision' | 'phase' | 'result';
export type TraceStatus = 'ok' | 'error';

/**
 * Одно событие трассы. Тело (промпт и ответ) приходит только в детальном
 * запросе — в ленте его нет, там лишь признак has_payload.
 */
export interface TrajectoryEvent {
	seq: number;
	at: string;
	kind: TraceKind;
	name: string;
	status: TraceStatus;
	summary: string;
	duration_ms: number;
	prompt_tokens: number;
	completion_tokens: number;
	has_payload: boolean;
	error?: string;
	payload?: unknown;
}

/** Лента событий прогона: что делали агенты и инструменты. */
export interface Trajectory {
	next_seq?: number;
	has_more?: boolean;
	id: string;
	total: number;
	events: TrajectoryEvent[];
}

// --- интервью по брифу (SSE) ---

/** Черновик брифа, который интервьюер собирает по ходу разговора. */
export interface BriefDraft {
	product: string;
	goal: string;
	audience: string;
	tone: string;
	/** geo ID Яндекса числом в строке: так его отдаёт и принимает бэкенд. */
	region?: string;
	topics_count?: number;
}

/** Одна реплика диалога: и в теле запроса, и в ленте. */
export interface InterviewMessage {
	role: 'user' | 'assistant';
	content: string;
}

/** Готовность брифа: считает сервер по четырём обязательным полям. */
export type InterviewStatus = 'ready' | 'needs_input';

export interface InterviewDeltaFrame {
	type: 'delta';
	text: string;
}

export interface InterviewBriefFrame {
	type: 'brief';
	brief: BriefDraft;
	/** Машинные ключи незаполненных полей: product, goal, audience, tone. */
	missing: string[];
	status: InterviewStatus;
}

export interface InterviewErrorFrame {
	type: 'error';
	message: string;
}

export interface InterviewDoneFrame {
	type: 'done';
}

/** Кадр потока интервью — публичный контракт эндпоинта. */
export type InterviewFrame = InterviewDeltaFrame | InterviewBriefFrame | InterviewErrorFrame | InterviewDoneFrame;
