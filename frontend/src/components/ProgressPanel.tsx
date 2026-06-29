import type { Snapshot, TopicState } from '../api/client'

const PHASE_LABEL: Record<string, string> = {
  strategizing: 'Стратегия',
  producing: 'Генерация статей',
  done: 'Готово',
  failed: 'Ошибка',
}

const TOPIC_LABEL: Record<TopicState, string> = {
  pending: 'в очереди',
  writing: 'пишется',
  reviewing: 'на ревью',
  revising: 'доработка',
  done: 'готово',
}

function topicSuffix(state: TopicState, iter?: number, score?: number): string {
  if ((state === 'reviewing' || state === 'revising') && iter) return ` · итер. ${iter}`
  if (state === 'done' && score != null) return ` · ${score}`
  return ''
}

export function ProgressPanel({ product, snapshot }: { product: string; snapshot: Snapshot | null }) {
  return (
    <div className="progress">
      <h2>{product}</h2>
      <p>{snapshot ? PHASE_LABEL[snapshot.phase] : 'Подключение…'}</p>
      <div className="bar">
        <div className="bar-fill" style={{ width: `${snapshot?.percent ?? 0}%` }} />
      </div>
      <ul className="topics">
        {snapshot?.topics.map((t) => (
          <li key={t.index} className={`topic topic-${t.state}`}>
            <span className="topic-title">{t.title}</span>
            <span className="topic-state">
              {TOPIC_LABEL[t.state]}
              {topicSuffix(t.state, t.iter, t.score)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
