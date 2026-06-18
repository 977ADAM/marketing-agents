import { render, screen, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { CampaignView } from './CampaignView'
import type { Campaign } from '../api/client'

const getCampaign = vi.fn()
vi.mock('../api/client', () => ({ getCampaign: (id: string) => getCampaign(id) }))

function renderView() {
  render(
    <MemoryRouter initialEntries={['/campaigns/x']}>
      <Routes>
        <Route path="/campaigns/:id" element={<CampaignView />} />
      </Routes>
    </MemoryRouter>,
  )
}

const base: Campaign = {
  id: 'x', client_id: 'c', status: 'running',
  brief: { product: 'Вода', goal: '', audience: '', tone: '' },
  created_at: '', updated_at: '',
}

beforeEach(() => {
  vi.useFakeTimers()
  getCampaign.mockReset()
})
afterEach(() => vi.useRealTimers())

describe('CampaignView polling', () => {
  it('shows progress while running, then renders result on done', async () => {
    getCampaign
      .mockResolvedValueOnce({ ...base, status: 'running' })
      .mockResolvedValueOnce({
        ...base,
        status: 'done',
        strategy: { positioning: 'Поз', topics: [] },
        cost_usd: 0.01,
        deliverables: [
          { topic: 't', title: 'Статья', body: 'b', cta: 'c', review: { score: 90, issues: [], verdict: 'accept' } },
        ],
      })
    renderView()

    await act(async () => { await Promise.resolve() })
    expect(screen.getByText('Генерация кампании…')).toBeInTheDocument()

    await act(async () => { vi.advanceTimersByTime(2500); await Promise.resolve() })
    expect(screen.getByText('Статья')).toBeInTheDocument()
    expect(screen.getByText('Поз')).toBeInTheDocument()
  })

  it('renders error on failed', async () => {
    getCampaign.mockResolvedValueOnce({ ...base, status: 'failed', error: 'boom' })
    renderView()
    await act(async () => { await Promise.resolve() })
    expect(screen.getByText('boom')).toBeInTheDocument()
  })
})
