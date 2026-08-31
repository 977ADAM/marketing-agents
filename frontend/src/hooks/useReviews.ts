import { useCallback, useEffect, useState } from 'react'
import { listReviews, type ReviewRunSummary } from '../api/client'

export function useReviews() {
  const [items, setItems] = useState<ReviewRunSummary[]>([])

  const refresh = useCallback(async () => {
    try {
      setItems(await listReviews())
    } catch {
      /* оставляем прежний список при сбое */
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  return { items, refresh }
}
