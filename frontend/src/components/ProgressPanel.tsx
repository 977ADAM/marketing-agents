import type { Snapshot, TopicState, Phase } from '../api/client'

const PHASE_LABEL: Record<Phase, string> = {
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

// ProgressPanel — общий прогресс прогона. Для проверки текстов можно переопределить
// подписи фаз/состояний (phaseLabels/topicLabels) и отключить итерации (showIter=false).
export function ProgressPanel({
  product,
  snapshot,
  phaseLabels,
  topicLabels,
  showIter = true,
}: {
  product: string
  snapshot: Snapshot | null
  phaseLabels?: Partial<Record<Phase, string>>
  topicLabels?: Partial<Record<TopicState, string>>
  showIter?: boolean
}) {
  const phase = (ph: Phase) => phaseLabels?.[ph] ?? PHASE_LABEL[ph]
  const topic = (st: TopicState) => topicLabels?.[st] ?? TOPIC_LABEL[st]
  return (
    <div className="progress">
      <h2>{product}</h2>
      <p>{snapshot ? phase(snapshot.phase) : 'Подключение…'}</p>
      <div
        className="bar"
        role="progressbar"
        aria-valuenow={snapshot?.percent ?? 0}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Прогресс прогона"
      >
        <div className="bar-fill" style={{ width: `${snapshot?.percent ?? 0}%` }} />
      </div>
      <ul className="topics">
        {(snapshot?.topics ?? []).map((t) => (
          <li key={t.index} className={`topic topic-${t.state}`}>
            <span className="topic-title">{t.title}</span>
            <span className="topic-state">
              {topic(t.state)}
              {showIter && topicSuffix(t.state, t.iter, t.score)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
