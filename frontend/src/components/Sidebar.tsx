import { Link, useNavigate } from 'react-router-dom'
import type { CampaignSummary, ReviewRunSummary } from '../api/client'
import { StatusChip } from './StatusChip'

function briefLabel(brief: string): string {
  const first = brief.split('\n').find((l) => l.trim() !== '')
  const s = (first ?? 'Без названия').trim()
  return s.length > 40 ? s.slice(0, 40) + '…' : s
}

export function Sidebar({
  campaigns,
  reviews,
}: {
  campaigns: CampaignSummary[]
  reviews: ReviewRunSummary[]
}) {
  const nav = useNavigate()
  return (
    <aside className="sidebar">
      <button className="new-btn" onClick={() => nav('/')}>+ Новая кампания</button>
      <button className="new-btn secondary" onClick={() => nav('/reviews')}>✎ Проверить тексты</button>
      <div className="history-label">Кампании</div>
      <ul className="history">
        {campaigns.map((c) => (
          <li key={c.id}>
            <Link to={`/campaigns/${c.id}`}>
              <span className="hist-product">{c.brief.product || 'без названия'}</span>
              <StatusChip status={c.status} />
            </Link>
          </li>
        ))}
      </ul>
      <div className="history-label">Проверки текстов</div>
      <ul className="history">
        {reviews.map((r) => (
          <li key={r.id}>
            <Link to={`/reviews/${r.id}`}>
              <span className="hist-product">{briefLabel(r.brief_text)}</span>
              <StatusChip status={r.status} runningLabel="Проверка" />
            </Link>
          </li>
        ))}
        {reviews.length === 0 && <li className="muted">Пока пусто</li>}
      </ul>
    </aside>
  )
}
