import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect } from 'vitest'
import { Sidebar } from './Sidebar'
import type { CampaignSummary } from '../api/client'

const items: CampaignSummary[] = [
  { id: '1', status: 'done', brief: { product: 'Вода', goal: '', audience: '', tone: '' }, created_at: '' },
  { id: '2', status: 'running', brief: { product: 'CRM', goal: '', audience: '', tone: '' }, created_at: '' },
]

describe('Sidebar', () => {
  it('renders history items with links', () => {
    render(<MemoryRouter><Sidebar items={items} /></MemoryRouter>)
    expect(screen.getByText('Вода')).toBeInTheDocument()
    expect(screen.getByText('CRM')).toBeInTheDocument()
    const link = screen.getByText('Вода').closest('a')
    expect(link).toHaveAttribute('href', '/campaigns/1')
  })
})
