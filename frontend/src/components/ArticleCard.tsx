import { useState } from 'react'
import type { Deliverable } from '../api/client'

export function scoreColor(score: number): 'green' | 'amber' | 'red' {
  if (score >= 80) return 'green'
  if (score >= 60) return 'amber'
  return 'red'
}

export function ArticleCard({ d }: { d: Deliverable }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="article-card">
      <div className="article-head">
        <h3>{d.title}</h3>
        <span className={`score score-${scoreColor(d.review.score)}`}>{d.review.score}</span>
      </div>
      <p className="verdict">{d.review.verdict === 'accept' ? 'Принято' : 'На доработку'}</p>
      <p className="cta">{d.cta}</p>
      <button onClick={() => setOpen(true)}>Читать ▸</button>
      {open && (
        <div className="modal" role="dialog" onClick={() => setOpen(false)}>
          <div className="modal-body" onClick={(e) => e.stopPropagation()}>
            <h3>{d.title}</h3>
            <p className="article-body">{d.body}</p>
            {d.review.issues?.length > 0 && (
              <>
                <h4>Замечания критика</h4>
                <ul>{d.review.issues.map((i, k) => <li key={k}>{i}</li>)}</ul>
              </>
            )}
            <p className="cta">{d.cta}</p>
            <button onClick={() => setOpen(false)}>Закрыть</button>
          </div>
        </div>
      )}
    </div>
  )
}
