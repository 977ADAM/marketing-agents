export type Status = 'pending' | 'running' | 'done' | 'failed'

export interface Brief { product: string; goal: string; audience: string; tone: string }
export interface Topic { title: string; angle: string; points: string[] }
export interface Strategy { positioning: string; topics: Topic[] }
// issues опционален: Go сериализует nil-срез как null, не как [].
export interface Review { score: number; issues?: string[]; verdict: string }
export interface Deliverable { topic: string; title: string; body: string; cta: string; review: Review }

export interface Campaign {
  id: string
  client_id: string
  status: Status
  brief: Brief
  strategy?: Strategy
  deliverables?: Deliverable[]
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

// Базовый префикс API повторяет base сборки (import.meta.env.BASE_URL уже
// оканчивается на '/'): standalone → '/api', под interpool → '/marketing/api'.
const API = `${import.meta.env.BASE_URL}api`

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
