import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ProgressPanel } from './ProgressPanel'
import type { Snapshot } from '../api/client'

const snap: Snapshot = {
  phase: 'producing',
  percent: 60,
  topic_total: 2,
  topics_done: 1,
  topics: [
    { index: 0, title: 'Тема 1', state: 'done', score: 88 },
    { index: 1, title: 'Тема 2', state: 'reviewing', iter: 2 },
  ],
}

describe('ProgressPanel', () => {
  it('рендерит фазу, темы и состояния', () => {
    render(<ProgressPanel product="Вода" snapshot={snap} />)
    expect(screen.getByText('Вода')).toBeInTheDocument()
    expect(screen.getByText('Генерация статей')).toBeInTheDocument()
    expect(screen.getByText('Тема 1')).toBeInTheDocument()
    expect(screen.getByText(/готово · 88/)).toBeInTheDocument()
    expect(screen.getByText(/на ревью · итер\. 2/)).toBeInTheDocument()
  })

  it('показывает заглушку без снимка', () => {
    render(<ProgressPanel product="Вода" snapshot={null} />)
    expect(screen.getByText('Подключение…')).toBeInTheDocument()
  })

  it('не падает на снимке strategizing с topics=null', () => {
    const strat = { phase: 'strategizing', percent: 5, topic_total: 0, topics_done: 0, topics: null } as unknown as Snapshot
    render(<ProgressPanel product="Вода" snapshot={strat} />)
    expect(screen.getByText('Стратегия')).toBeInTheDocument()
  })
})
