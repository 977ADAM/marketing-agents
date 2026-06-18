import { describe, it, expect, vi, afterEach } from 'vitest'
import { createCampaign, ApiError } from './client'

afterEach(() => vi.restoreAllMocks())

describe('createCampaign', () => {
  it('POSTs brief and returns id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'abc', status: 'pending' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const res = await createCampaign({ product: 'P', goal: 'G', audience: 'A', tone: 'T' })
    expect(res.id).toBe('abc')
    expect(fetchMock).toHaveBeenCalledWith('/api/campaigns', expect.objectContaining({ method: 'POST' }))
  })

  it('throws ApiError with code/message from body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      json: async () => ({ error: { code: 'validation', message: 'bad' } }),
    }))
    await expect(createCampaign({ product: '', goal: '', audience: '', tone: '' }))
      .rejects.toMatchObject({ code: 'validation', message: 'bad' } satisfies Partial<ApiError>)
  })
})
