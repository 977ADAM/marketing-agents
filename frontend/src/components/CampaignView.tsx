import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { getCampaign, type Campaign } from '../api/client'
import { useCampaignProgress } from '../hooks/useCampaignProgress'
import { ArticleCard } from './ArticleCard'
import { ProgressPanel } from './ProgressPanel'

export function CampaignView() {
  const { id = '' } = useParams()
  const { snapshot, terminal, error } = useCampaignProgress(id)
  const [campaign, setCampaign] = useState<Campaign | null>(null)

  // Загружаем полную кампанию при монтировании и повторно на терминальном
  // событии. Флаг cancelled гасит устаревший ответ (медленный первичный fetch
  // не должен перезаписать уже полученный финальный результат).
  useEffect(() => {
    let cancelled = false
    getCampaign(id)
      .then((c) => { if (!cancelled) setCampaign(c) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [id, terminal])

  if (!campaign) return <p className="muted">Загрузка…</p>

  if (campaign.status === 'failed') {
    return (
      <div className="failed">
        <h2>Ошибка</h2>
        <p className="error">{campaign.error}</p>
      </div>
    )
  }

  if (campaign.status === 'done') {
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

  // pending / running — живой прогресс
  return (
    <>
      <ProgressPanel product={campaign.brief.product} snapshot={snapshot} />
      {error && <p className="muted">Переподключение…</p>}
    </>
  )
}
