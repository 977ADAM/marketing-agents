import { useEffect, useState } from 'react'
import { eventsUrl, type Snapshot } from '../api/client'

export function useCampaignProgress(id: string) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [terminal, setTerminal] = useState(false)

  useEffect(() => {
    setSnapshot(null)
    setTerminal(false)
    const es = new EventSource(eventsUrl(id))

    es.onmessage = (e) => {
      setSnapshot(JSON.parse(e.data) as Snapshot)
    }
    es.addEventListener('done', (e) => {
      setSnapshot(JSON.parse((e as MessageEvent).data) as Snapshot)
      setTerminal(true)
      es.close()
    })

    return () => es.close()
  }, [id])

  return { snapshot, terminal }
}
