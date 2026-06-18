import { Link, useNavigate } from 'react-router-dom'
import type { CampaignSummary } from '../api/client'
import { StatusChip } from './StatusChip'

export function Sidebar({ items }: { items: CampaignSummary[] }) {
  const nav = useNavigate()
  return (
    <aside className="sidebar">
      <button className="new-btn" onClick={() => nav('/')}>+ Новая кампания</button>
      <div className="history-label">История</div>
      <ul className="history">
        {items.map((c) => (
          <li key={c.id}>
            <Link to={`/campaigns/${c.id}`}>
              <span className="hist-product">{c.brief.product || 'без названия'}</span>
              <StatusChip status={c.status} />
            </Link>
          </li>
        ))}
      </ul>
    </aside>
  )
}
