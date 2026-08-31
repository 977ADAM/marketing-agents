import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect } from 'vitest'
import { Sidebar } from './Sidebar'
import type { CampaignSummary, ReviewRunSummary } from '../api/client'

const campaigns: CampaignSummary[] = [
  { id: '1', status: 'done', brief: { product: 'Вода', goal: '', audience: '', tone: '' }, created_at: '' },
  { id: '2', status: 'running', brief: { product: 'CRM', goal: '', audience: '', tone: '' }, created_at: '' },
]

const reviews: ReviewRunSummary[] = [
  { id: 'r1', status: 'done', brief_text: 'Бриф на зимние шины', created_at: '' },
]

describe('Sidebar', () => {
  it('renders campaign history items with links', () => {
    render(<MemoryRouter><Sidebar campaigns={campaigns} reviews={[]} /></MemoryRouter>)
    expect(screen.getByText('Вода')).toBeInTheDocument()
    expect(screen.getByText('CRM')).toBeInTheDocument()
    const link = screen.getByText('Вода').closest('a')
    expect(link).toHaveAttribute('href', '/campaigns/1')
  })

  it('renders reviews history and link to review form', () => {
    render(<MemoryRouter><Sidebar campaigns={[]} reviews={reviews} /></MemoryRouter>)
    expect(screen.getByText('Бриф на зимние шины')).toBeInTheDocument()
    expect(screen.getByText('✎ Проверить тексты')).toBeInTheDocument()
  })
})
