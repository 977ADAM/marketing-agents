import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ReviewForm } from './ReviewForm'

const navigate = vi.fn()
vi.mock('react-router-dom', async (orig) => ({
  ...(await orig<typeof import('react-router-dom')>()),
  useNavigate: () => navigate,
}))

const createReview = vi.fn()
const extractDocx = vi.fn()
vi.mock('../api/client', () => ({
  createReview: (b: unknown) => createReview(b),
  extractDocx: (f: unknown) => extractDocx(f),
}))

beforeEach(() => {
  navigate.mockReset()
  createReview.mockReset()
  extractDocx.mockReset()
})

function fillBrief(text: string) {
  fireEvent.change(screen.getByLabelText(/Бриф/), { target: { value: text } })
}

function fillFirstBody(text: string) {
  fireEvent.change(screen.getAllByPlaceholderText('Текст статьи…')[0], { target: { value: text } })
}

describe('ReviewForm', () => {
  it('disables submit until brief and at least one body are filled', () => {
    render(<MemoryRouter><ReviewForm /></MemoryRouter>)
    const btn = screen.getByRole('button', { name: /Проверить агентами/ })
    expect(btn).toBeDisabled()
    fillBrief('бриф')
    expect(btn).toBeDisabled()
    fillFirstBody('текст статьи')
    expect(btn).toBeEnabled()
  })

  it('submits and navigates to the review', async () => {
    createReview.mockResolvedValue({ id: 'rev-1', status: 'pending' })
    render(<MemoryRouter><ReviewForm /></MemoryRouter>)
    fillBrief('бриф')
    fillFirstBody('текст статьи')
    fireEvent.click(screen.getByRole('button', { name: /Проверить агентами/ }))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/reviews/rev-1'))
    expect(createReview).toHaveBeenCalledWith(
      expect.objectContaining({ brief: 'бриф', texts: [{ title: '', body: 'текст статьи' }] }),
    )
  })

  it('fills brief from extracted docx', async () => {
    extractDocx.mockResolvedValue({ title: 'Бриф', text: 'Бриф на шины\nМного текста' })
    const { container } = render(<MemoryRouter><ReviewForm /></MemoryRouter>)
    fireEvent.click(screen.getByText(/Загрузить бриф/))
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['x'], 'brief.docx')] } })
    await waitFor(() =>
      expect(screen.getByLabelText(/Бриф/)).toHaveValue('Бриф на шины\nМного текста'),
    )
  })

  it('shows error when docx extraction fails', async () => {
    extractDocx.mockRejectedValue(new Error('не docx'))
    const { container } = render(<MemoryRouter><ReviewForm /></MemoryRouter>)
    fireEvent.click(screen.getByText(/Загрузить бриф/))
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['x'], 'bad.docx')] } })
    await waitFor(() => expect(screen.getByText(/не docx/)).toBeInTheDocument())
  })
})
