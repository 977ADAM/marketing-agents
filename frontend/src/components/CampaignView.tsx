import { useParams } from 'react-router-dom'
import { useCampaign } from '../hooks/useCampaign'
import { ArticleCard } from './ArticleCard'

const STEPS = ['Стратег', 'Копирайтеры', 'Критик']

export function CampaignView() {
  const { id = '' } = useParams()
  const { campaign, error } = useCampaign(id)

  if (!campaign) return <p className="muted">Загрузка…</p>

  if (campaign.status === 'pending' || campaign.status === 'running') {
    const active = campaign.strategy ? 1 : 0
    return (
      <div className="progress">
        <h2>{campaign.brief.product}</h2>
        <p>Генерация кампании…</p>
        <ol className="steps">
          {STEPS.map((s, i) => (
            <li key={s} className={i <= active ? 'active' : ''}>{s}</li>
          ))}
        </ol>
        {error && <p className="muted">Переподключение…</p>}
      </div>
    )
  }

  if (campaign.status === 'failed') {
    return (
      <div className="failed">
        <h2>Ошибка</h2>
        <p className="error">{campaign.error}</p>
      </div>
    )
  }

  // done
  return (
    <div className="result">
      <h2>{campaign.brief.product}</h2>
      {campaign.strategy && (
        <div className="positioning">
          <h3>Позиционирование</h3>
          <p>{campaign.strategy.positioning}</p>
        </div>
      )}
      {campaign.cost_usd != null && (
        <p className="muted">Стоимость прогона: ${campaign.cost_usd.toFixed(4)}</p>
      )}
      <div className="articles">
        {campaign.deliverables?.map((d, i) => <ArticleCard key={i} d={d} />)}
      </div>
    </div>
  )
}
