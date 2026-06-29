import { useEffect, useState } from 'react'
import { eventsUrl, type Snapshot } from '../api/client'

export function useCampaignProgress(id: string) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [terminal, setTerminal] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setSnapshot(null)
    setTerminal(false)
    setError(null)
    const es = new EventSource(eventsUrl(id))

    es.onmessage = (e) => {
      setError(null)
      setSnapshot(JSON.parse(e.data) as Snapshot)
    }
    es.addEventListener('done', (e) => {
      setSnapshot(JSON.parse(e.data) as Snapshot)
      setTerminal(true)
      es.close() // прогон завершён — гасим авто-реконнект EventSource
    })
    es.onerror = () => setError('reconnecting') // EventSource переподключится сам

    return () => es.close()
  }, [id])

  return { snapshot, terminal, error }
}
