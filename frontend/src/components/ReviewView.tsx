import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { getReview, type ReviewRun, type TextReport, type CheckScore } from '../api/client'
import { useReviewProgress } from '../hooks/useReviewProgress'
import { ProgressPanel } from './ProgressPanel'

const PHASE_LABELS = { producing: 'Проверка текстов', done: 'Готово', failed: 'Ошибка' }
const TOPIC_LABELS = {
  writing: 'соответствие брифу',
  reviewing: 'корректность текста',
  done: 'проверено',
}

function scoreClass(score: number): string {
  if (score >= 80) return 'score-green'
  if (score >= 60) return 'score-amber'
  return 'score-red'
}

function CheckBlock({ label, check }: { label: string; check: CheckScore }) {
  return (
    <div className="check-block">
      <div className="check-head">
        <span>{label}</span>
        <span className={`score ${scoreClass(check.score)}`}>{check.score}</span>
      </div>
      {(check.issues ?? []).length > 0 ? (
        <ul className="issues">
          {check.issues!.map((issue, i) => (
            <li key={i}>{issue}</li>
          ))}
        </ul>
      ) : (
        <p className="muted">Замечаний нет</p>
      )}
    </div>
  )
}

function ReportCard({ report }: { report: TextReport }) {
  return (
    <div className={`report-card ${report.verdict === 'pass' ? 'report-pass' : 'report-fix'}`}>
      <div className="report-head">
        <span className="report-title">{report.title || 'Без заголовка'}</span>
        <span className="report-verdict">
          {report.verdict === 'pass' ? '✅ готово к публикации' : '⚠️ требует доработки'}
        </span>
      </div>
      <div className="report-overall">
        Итог: <span className={`score ${scoreClass(report.overall)}`}>{report.overall}</span>
      </div>
      <CheckBlock label="Соответствие брифу" check={report.compliance} />
      <CheckBlock label="Корректность текста" check={report.quality} />
    </div>
  )
}

export function ReviewView() {
  const { id = '' } = useParams()
  const { snapshot, terminal } = useReviewProgress(id)
  const [review, setReview] = useState<ReviewRun | null>(null)

  useEffect(() => {
    let cancelled = false
    getReview(id)
      .then((r) => { if (!cancelled) setReview(r) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [id, terminal])

  if (!review) return <p className="muted">Загрузка…</p>

  if (review.status === 'failed') {
    return (
      <div className="failed">
        <h2>Ошибка проверки</h2>
        <p className="error">{review.error}</p>
      </div>
    )
  }

  if (review.status === 'done' && review.result) {
    const passed = review.result.items.filter((i) => i.verdict === 'pass').length
    return (
      <div className="result">
        <h2>Отчёт по текстам</h2>
        <p className="muted">
          Проверено текстов: {review.result.items.length}, прошло: {passed} · Стоимость: $
          {review.cost_usd?.toFixed(4) ?? '—'}
        </p>
        <details className="brief-box">
          <summary>Бриф</summary>
          <p className="brief-text">{review.brief_text}</p>
        </details>
        <div className="reports">
          {review.result.items.map((r, i) => (
            <ReportCard key={i} report={r} />
          ))}
        </div>
      </div>
    )
  }

  // pending / running — живой прогресс
  return <ProgressPanel product="Проверка текстов" snapshot={snapshot} phaseLabels={PHASE_LABELS} topicLabels={TOPIC_LABELS} showIter={false} />
}
