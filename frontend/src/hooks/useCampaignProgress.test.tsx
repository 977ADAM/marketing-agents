import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useCampaignProgress } from './useCampaignProgress'

class MockEventSource {
  static last: MockEventSource = null!
  url: string
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
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
  emitError() {
    this.onerror?.(new Event('error'))
  }
}

beforeEach(() => {
  MockEventSource.last = null!
  vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
})
afterEach(() => vi.unstubAllGlobals())

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

  it('закрывает EventSource при размонтировании', () => {
    const { unmount } = renderHook(() => useCampaignProgress('c1'))
    const es = MockEventSource.last
    unmount()
    expect(es.closed).toBe(true)
  })

  it('сбрасывает состояние при смене id', () => {
    const { result, rerender } = renderHook(({ id }) => useCampaignProgress(id), {
      initialProps: { id: 'c1' },
    })
    act(() => {
      MockEventSource.last.emit({ phase: 'producing', topics: [], topic_total: 1, topics_done: 0, percent: 50 })
    })
    expect(result.current.snapshot?.percent).toBe(50)

    rerender({ id: 'c2' })
    expect(result.current.snapshot).toBeNull()
    expect(result.current.terminal).toBe(false)
  })

  it('выставляет error при onerror и очищает на следующем сообщении', () => {
    const { result } = renderHook(() => useCampaignProgress('c1'))
    act(() => { MockEventSource.last.emitError() })
    expect(result.current.error).toBe('reconnecting')
    act(() => {
      MockEventSource.last.emit({ phase: 'producing', topics: [], topic_total: 1, topics_done: 0, percent: 10 })
    })
    expect(result.current.error).toBeNull()
  })
})
