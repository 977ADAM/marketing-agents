import { useEffect, useRef, useState } from 'react'
import { getCampaign, type Campaign } from '../api/client'

const POLL_MS = 2500
const TERMINAL = new Set<Campaign['status']>(['done', 'failed'])

export function useCampaign(id: string) {
  const [campaign, setCampaign] = useState<Campaign | null>(null)
  const [error, setError] = useState<string | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => {
    let cancelled = false

    async function tick() {
      try {
        const c = await getCampaign(id)
        if (cancelled) return
        setCampaign(c)
        setError(null)
        if (!TERMINAL.has(c.status)) {
          timer.current = setTimeout(tick, POLL_MS)
        }
      } catch (e) {
        if (cancelled) return
        setError((e as Error).message)
        timer.current = setTimeout(tick, POLL_MS) // ретрай при сетевом сбое
      }
    }

    tick()
    return () => {
      cancelled = true
      if (timer.current) clearTimeout(timer.current)
    }
  }, [id])

  return { campaign, error }
}
