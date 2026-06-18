import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { createCampaign, type Brief } from '../api/client'

const EMPTY: Brief = { product: '', goal: '', audience: '', tone: '' }

export function NewCampaign({ onCreated }: { onCreated?: () => void }) {
  const [brief, setBrief] = useState<Brief>(EMPTY)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const nav = useNavigate()

  const valid =
    brief.product.trim() !== '' &&
    brief.goal.trim() !== '' &&
    brief.audience.trim() !== '' &&
    brief.tone.trim() !== ''

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!valid || busy) return
    setBusy(true)
    setErr(null)
    try {
      const { id } = await createCampaign(brief)
      onCreated?.()
      nav(`/campaigns/${id}`)
    } catch (err) {
      setErr((err as Error).message)
      setBusy(false)
    }
  }

  function field(key: keyof Brief, label: string) {
    return (
      <label>
        {label}
        <textarea
          value={brief[key]}
          onChange={(e) => setBrief((prev) => ({ ...prev, [key]: e.target.value }))}
        />
      </label>
    )
  }

  return (
    <form className="new-campaign" onSubmit={submit}>
      <h2>Новая кампания</h2>
      {field('product', 'Продукт / бренд')}
      {field('goal', 'Цель кампании')}
      {field('audience', 'Аудитория')}
      {field('tone', 'Tone of voice')}
      {err && <p className="error">{err}</p>}
      <button type="submit" disabled={!valid || busy}>
        {busy ? 'Создаём…' : 'Сгенерировать →'}
      </button>
    </form>
  )
}
