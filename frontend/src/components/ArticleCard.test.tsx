import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ArticleCard, scoreColor } from './ArticleCard'
import type { Deliverable } from '../api/client'

const d: Deliverable = {
  topic: 'T', title: 'Заголовок', body: 'Тело статьи', cta: 'Купить',
  review: { score: 95, issues: ['мелочь'], verdict: 'accept' },
}

describe('scoreColor', () => {
  it('maps thresholds', () => {
    expect(scoreColor(95)).toBe('green')
    expect(scoreColor(80)).toBe('green')
    expect(scoreColor(70)).toBe('amber')
    expect(scoreColor(60)).toBe('amber')
    expect(scoreColor(40)).toBe('red')
  })
})

describe('ArticleCard', () => {
  it('shows score and reveals body + issues on Читать', () => {
    render(<ArticleCard d={d} />)
    expect(screen.getByText('95')).toBeInTheDocument()
    expect(screen.queryByText('Тело статьи')).not.toBeInTheDocument()
    fireEvent.click(screen.getByText('Читать ▸'))
    expect(screen.getByText('Тело статьи')).toBeInTheDocument()
    expect(screen.getByText('мелочь')).toBeInTheDocument()
  })
})
