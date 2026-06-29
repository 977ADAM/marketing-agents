import { render, screen, act, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { CampaignView } from './CampaignView'
import type { Campaign } from '../api/client'

const getCampaign = vi.fn()
vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>()
  return { ...actual, getCampaign: (id: string) => getCampaign(id) }
})

class MockEventSource {
  static last: MockEventSource = null!
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  listeners: Record<string, (e: MessageEvent) => void> = {}
  closed = false
  constructor() {
    MockEventSource.last = this
  }
  addEventListener(type: string, cb: (e: MessageEvent) => void) {
    this.listeners[type] = cb
  }
  close() {
    this.closed = true
  }
  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent)
  }
  emitEvent(type: string, data: unknown) {
    this.listeners[type]?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const running: Campaign = {
  id: 'x', client_id: 'c', status: 'running',
  brief: { product: 'Вода', goal: '', audience: '', tone: '' },
  created_at: '', updated_at: '',
}

function renderView() {
  render(
    <MemoryRouter initialEntries={['/campaigns/x']}>
      <Routes>
        <Route path="/campaigns/:id" element={<CampaignView />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  getCampaign.mockReset()
  MockEventSource.last = null!
  vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
})

describe('CampaignView', () => {
  it('показывает прогресс, затем результат после terminal-события', async () => {
    getCampaign.mockResolvedValueOnce(running) // первичная загрузка
    renderView()

    await waitFor(() => expect(screen.getByText('Вода')).toBeInTheDocument())

    act(() => {
      MockEventSource.last.emit({ phase: 'producing', topics: [{ index: 0, title: 'Тема', state: 'writing' }], topic_total: 1, topics_done: 0, percent: 30 })
    })
    expect(screen.getByText('Тема')).toBeInTheDocument()

    getCampaign.mockResolvedValueOnce({
      ...running, status: 'done',
      strategy: { positioning: 'Поз', topics: [] },
      cost_usd: 0.01,
      deliverables: [{ topic: 't', title: 'Статья', body: 'b', cta: 'c', review: { score: 90, issues: [], verdict: 'accept' } }],
    })
    act(() => {
      MockEventSource.last.emitEvent('done', { phase: 'done', topics: [], topic_total: 1, topics_done: 1, percent: 100 })
    })

    await waitFor(() => expect(screen.getByText('Статья')).toBeInTheDocument())
    expect(screen.getByText('Поз')).toBeInTheDocument()
  })

  it('рендерит ошибку для failed-кампании', async () => {
    getCampaign.mockResolvedValue({ ...running, status: 'failed', error: 'boom' })
    renderView()
    await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument())
  })
})
