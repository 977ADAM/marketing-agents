import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useCampaignProgress } from './useCampaignProgress'

class MockEventSource {
  static last: MockEventSource
  url: string
  onmessage: ((e: MessageEvent) => void) | null = null
  listeners: Record<string, (e: MessageEvent) => void> = {}
  closed = false
  constructor(url: string) {
    this.url = url
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

beforeEach(() => {
  vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
})

describe('useCampaignProgress', () => {
  it('обновляет снимок из onmessage и помечает terminal на done', () => {
    const { result } = renderHook(() => useCampaignProgress('c1'))
    expect(result.current.snapshot).toBeNull()

    act(() => {
      MockEventSource.last.emit({ phase: 'producing', topics: [], topic_total: 1, topics_done: 0, percent: 30 })
    })
    expect(result.current.snapshot?.percent).toBe(30)
    expect(result.current.terminal).toBe(false)

    act(() => {
      MockEventSource.last.emitEvent('done', { phase: 'done', topics: [], topic_total: 1, topics_done: 1, percent: 100 })
    })
    expect(result.current.terminal).toBe(true)
    expect(result.current.snapshot?.percent).toBe(100)
    expect(MockEventSource.last.closed).toBe(true)
  })
})
