import { useEffect, useState } from 'react'
import { reviewEventsUrl, type Snapshot } from '../api/client'

// Прогресс проверки текстов через SSE (тот же протокол, что у кампаний).
export function useReviewProgress(id: string) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [terminal, setTerminal] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setSnapshot(null)
    setTerminal(false)
    setError(null)
    const es = new EventSource(reviewEventsUrl(id))

    es.onmessage = (e) => {
      setError(null)
      setSnapshot(JSON.parse(e.data) as Snapshot)
    }
    es.addEventListener('done', (e) => {
      setSnapshot(JSON.parse(e.data) as Snapshot)
      setTerminal(true)
      es.close()
    })
    es.onerror = () => setError('reconnecting')

    return () => es.close()
  }, [id])

  return { snapshot, terminal, error }
}
