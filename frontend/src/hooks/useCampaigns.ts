import { useCallback, useEffect, useState } from 'react'
import { listCampaigns, type CampaignSummary } from '../api/client'

export function useCampaigns() {
  const [items, setItems] = useState<CampaignSummary[]>([])

  const refresh = useCallback(async () => {
    try {
      setItems(await listCampaigns())
    } catch {
      /* оставляем прежний список при сбое */
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  return { items, refresh }
}
