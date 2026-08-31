import { useRef, useState, type ChangeEvent, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { createReview, extractDocx, type ReviewText } from '../api/client'

const EMPTY_TEXT: ReviewText = { title: '', body: '' }

export function ReviewForm({ onCreated }: { onCreated?: () => void }) {
  const [brief, setBrief] = useState('')
  const [texts, setTexts] = useState<ReviewText[]>([{ ...EMPTY_TEXT }])
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const nav = useNavigate()

  const briefFileRef = useRef<HTMLInputElement>(null)
  const textFileRefs = useRef<(HTMLInputElement | null)[]>([])

  const valid = brief.trim() !== '' && texts.length > 0 && texts.every((t) => t.body.trim() !== '')

  function setText(i: number, patch: Partial<ReviewText>) {
    setTexts((prev) => prev.map((t, idx) => (idx === i ? { ...t, ...patch } : t)))
  }

  function addText() {
    setTexts((prev) => [...prev, { ...EMPTY_TEXT }])
  }

  function removeText(i: number) {
    setTexts((prev) => prev.filter((_, idx) => idx !== i))
  }

  // Общий обработчик загрузки .docx: для брифа и для отдельных текстов.
  async function handleDocx(e: ChangeEvent<HTMLInputElement>, target: 'brief' | number) {
    const file = e.target.files?.[0]
    e.target.value = '' // позволяем повторно выбрать тот же файл
    if (!file) return
    setErr(null)
    try {
      const doc = await extractDocx(file)
      if (target === 'brief') {
        setBrief(doc.text)
      } else {
        // первая строка .docx — обычно заголовок статьи
        const nl = doc.text.indexOf('\n')
        const title = nl >= 0 ? doc.text.slice(0, nl) : doc.title
        const body = nl >= 0 ? doc.text.slice(nl + 1) : doc.text
        setText(target, { title, body })
      }
    } catch (error) {
      setErr(`Не удалось разобрать .docx: ${(error as Error).message}`)
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!valid || busy) return
    setBusy(true)
    setErr(null)
    try {
      const { id } = await createReview({ brief, texts })
      onCreated?.()
      nav(`/reviews/${id}`)
    } catch (error) {
      setErr((error as Error).message)
      setBusy(false)
    }
  }

  return (
    <form className="new-campaign review-form" onSubmit={submit}>
      <h2>Проверка готовых текстов</h2>

      <label>
        Бриф (скопируйте текст брифа или загрузите .docx)
        <textarea
          value={brief}
          onChange={(e) => setBrief(e.target.value)}
          placeholder="Требования клиента: продукт, аудитория, УТП, запреты…"
        />
        <input
          ref={briefFileRef}
          type="file"
          accept=".docx"
          hidden
          onChange={(e) => void handleDocx(e, 'brief')}
        />
      </label>
      <button type="button" className="docx-btn" onClick={() => briefFileRef.current?.click()}>
        📄 Загрузить бриф (.docx)
      </button>

      <div className="review-texts">
        <div className="review-texts-label">Тексты для проверки</div>
        {texts.map((t, i) => (
          <div className="review-text" key={i}>
            <input
              value={t.title}
              onChange={(e) => setText(i, { title: e.target.value })}
              placeholder="Заголовок статьи"
            />
            <textarea
              value={t.body}
              onChange={(e) => setText(i, { body: e.target.value })}
              placeholder="Текст статьи…"
            />
            <div className="review-text-actions">
              <input
                ref={(el) => {
                  textFileRefs.current[i] = el
                }}
                type="file"
                accept=".docx"
                hidden
                onChange={(e) => void handleDocx(e, i)}
              />
              <button type="button" className="docx-btn" onClick={() => textFileRefs.current[i]?.click()}>
                📄 Загрузить статью (.docx)
              </button>
              {texts.length > 1 && (
                <button type="button" className="remove-btn" onClick={() => removeText(i)}>
                  Убрать
                </button>
              )}
            </div>
          </div>
        ))}
        <button type="button" className="docx-btn" onClick={addText}>
          + Добавить текст
        </button>
      </div>

      {err && <p className="error">{err}</p>}
      <button type="submit" disabled={!valid || busy}>
        {busy ? 'Проверяем…' : 'Проверить агентами →'}
      </button>
    </form>
  )
}
