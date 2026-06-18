import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { NewCampaign } from './NewCampaign'

const navigate = vi.fn()
vi.mock('react-router-dom', async (orig) => ({
  ...(await orig<typeof import('react-router-dom')>()),
  useNavigate: () => navigate,
}))

const createCampaign = vi.fn()
vi.mock('../api/client', () => ({ createCampaign: (b: unknown) => createCampaign(b) }))

beforeEach(() => {
  navigate.mockReset()
  createCampaign.mockReset()
})

function fill(label: RegExp, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } })
}

describe('NewCampaign', () => {
  it('disables submit until all fields filled', () => {
    render(<MemoryRouter><NewCampaign /></MemoryRouter>)
    const btn = screen.getByRole('button', { name: /Сгенерировать/ })
    expect(btn).toBeDisabled()
    fill(/Продукт/, 'Вода')
    fill(/Цель/, 'Продажи')
    fill(/Аудитория/, 'Все')
    fill(/Tone/, 'Дружелюбный')
    expect(btn).toBeEnabled()
  })

  it('submits and navigates to the new campaign', async () => {
    createCampaign.mockResolvedValue({ id: 'abc', status: 'pending' })
    render(<MemoryRouter><NewCampaign /></MemoryRouter>)
    fill(/Продукт/, 'Вода')
    fill(/Цель/, 'Продажи')
    fill(/Аудитория/, 'Все')
    fill(/Tone/, 'Дружелюбный')
    fireEvent.click(screen.getByRole('button', { name: /Сгенерировать/ }))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/campaigns/abc'))
  })
})
