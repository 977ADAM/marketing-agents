export type Status = 'pending' | 'running' | 'done' | 'failed'

export interface Brief { product: string; goal: string; audience: string; tone: string }
export interface Topic { title: string; angle: string; points: string[] }
export interface Strategy { positioning: string; topics: Topic[] }
// issues опционален: Go сериализует nil-срез как null, не как [].
export interface Review { score: number; issues?: string[]; verdict: string }
export interface Deliverable { topic: string; title: string; body: string; cta: string; review: Review }

export type Phase = 'strategizing' | 'producing' | 'done' | 'failed'
export type TopicState = 'pending' | 'writing' | 'reviewing' | 'revising' | 'done'
export interface TopicProgress {
  index: number
  title: string
  state: TopicState
  iter?: number
  score?: number
}
export interface Snapshot {
  phase: Phase
  topics: TopicProgress[]
  topic_total: number
  topics_done: number
  percent: number
}

export interface Campaign {
  id: string
  client_id: string
  status: Status
  brief: Brief
  strategy?: Strategy
  deliverables?: Deliverable[]
  progress?: Snapshot
  cost_usd?: number
  error?: string
  created_at: string
  updated_at: string
}

export interface CampaignSummary {
  id: string
  status: Status
  brief: Brief
  cost_usd?: number
  created_at: string
}

// --- Проверка готовых текстов ---

export interface ReviewText {
  title: string
  body: string
}

export interface ReviewRequest {
  brief: string
  texts: ReviewText[]
}

export interface CheckScore {
  score: number
  issues?: string[]
}

export interface TextReport {
  title: string
  compliance: CheckScore
  quality: CheckScore
  overall: number
  verdict: 'pass' | 'fix'
}

export interface ReviewResult {
  items: TextReport[]
  cost_usd: number
}

export interface ReviewRun {
  id: string
  client_id: string
  status: Status
  brief_text: string
  result?: ReviewResult
  progress?: Snapshot
  cost_usd?: number
  error?: string
  created_at: string
  updated_at: string
}

export interface ReviewRunSummary {
  id: string
  status: Status
  brief_text: string
  cost_usd?: number
  created_at: string
}

export interface ExtractedDoc {
  title: string
  text: string
}

// Базовый префикс API повторяет base сборки (import.meta.env.BASE_URL уже
// оканчивается на '/'): standalone → '/api', под interpool → '/marketing/api'.
const API = `${import.meta.env.BASE_URL}api`

export function eventsUrl(id: string): string {
  return `${API}/campaigns/${id}/events`
}

export function reviewEventsUrl(id: string): string {
  return `${API}/reviews/${id}/events`
}

export class ApiError extends Error {
  code: string
  constructor(code: string, message: string) {
    super(message)
    this.code = code
    this.name = 'ApiError'
  }
}

async function handle<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let code = `http_${res.status}`
    let message = res.statusText
    try {
      const body = await res.json()
      if (body?.error) {
        code = body.error.code ?? code
        message = body.error.message ?? message
      }
    } catch {
      /* тело не JSON — оставляем statusText */
    }
    throw new ApiError(code, message)
  }
  return res.json() as Promise<T>
}

export async function createCampaign(brief: Brief): Promise<{ id: string; status: Status }> {
  const res = await fetch(`${API}/campaigns`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(brief),
  })
  return handle(res)
}

export async function getCampaign(id: string): Promise<Campaign> {
  return handle(await fetch(`${API}/campaigns/${id}`))
}

export async function listCampaigns(): Promise<CampaignSummary[]> {
  return handle(await fetch(`${API}/campaigns`))
}

export async function createReview(req: ReviewRequest): Promise<{ id: string; status: Status }> {
  const res = await fetch(`${API}/reviews`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  return handle(res)
}

export async function getReview(id: string): Promise<ReviewRun> {
  return handle(await fetch(`${API}/reviews/${id}`))
}

export async function listReviews(): Promise<ReviewRunSummary[]> {
  return handle(await fetch(`${API}/reviews`))
}

// extractDocx загружает .docx на сервер и возвращает извлечённый текст.
export async function extractDocx(file: File): Promise<ExtractedDoc> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(`${API}/reviews/extract`, { method: 'POST', body: form })
  return handle(res)
}
