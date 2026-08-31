import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi } from 'vitest'
import { ReviewView } from './ReviewView'
import type { ReviewRun } from '../api/client'

const getReview = vi.fn()
vi.mock('../api/client', () => ({
  getReview: (id: string) => getReview(id),
  reviewEventsUrl: () => '/api/reviews/x/events',
}))
vi.mock('../hooks/useReviewProgress', () => ({
  useReviewProgress: () => ({ snapshot: null, terminal: false, error: null }),
}))

const doneReview: ReviewRun = {
  id: 'r1',
  client_id: 'c',
  status: 'done',
  brief_text: 'Бриф на шины',
  cost_usd: 0.0123,
  result: {
    cost_usd: 0.0123,
    items: [
      {
        title: 'Статья 1',
        overall: 85,
        verdict: 'pass',
        compliance: { score: 88, issues: [] },
        quality: { score: 85, issues: ['мелкая опечатка'] },
      },
      {
        title: 'Статья 2',
        overall: 70,
        verdict: 'fix',
        compliance: { score: 70, issues: ['не отражено УТП'] },
        quality: { score: 95, issues: [] },
      },
    ],
  },
  created_at: '',
  updated_at: '',
}

describe('ReviewView', () => {
  it('renders report cards with verdicts and issues', async () => {
    getReview.mockResolvedValue(doneReview)
    render(<MemoryRouter><ReviewView /></MemoryRouter>)
    expect(await screen.findByText('Статья 1')).toBeInTheDocument()
    expect(screen.getByText(/готово к публикации/)).toBeInTheDocument()
    expect(screen.getByText(/требует доработки/)).toBeInTheDocument()
    expect(screen.getByText('не отражено УТП')).toBeInTheDocument()
    expect(screen.getByText('мелкая опечатка')).toBeInTheDocument()
    expect(screen.getByText(/Проверено текстов: 2, прошло: 1/)).toBeInTheDocument()
  })

  it('shows failed state with error', async () => {
    getReview.mockResolvedValue({
      id: 'r2', client_id: 'c', status: 'failed', brief_text: 'б',
      error: 'llm: exhausted retries', created_at: '', updated_at: '',
    })
    render(<MemoryRouter><ReviewView /></MemoryRouter>)
    expect(await screen.findByText('llm: exhausted retries')).toBeInTheDocument()
  })
})
