import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import worker, { Outcome, PENDING_PREFIX, deliver, enqueue, retryPending } from './index.js'

/** In-memory stand-in for a KV namespace (put/get/list/delete with metadata). */
function fakeKV() {
  const store = new Map()
  return {
    store,
    async put(key, value, options = {}) {
      store.set(key, { value, metadata: options.metadata, ttl: options.expirationTtl })
    },
    async get(key) {
      return store.get(key)?.value ?? null
    },
    async delete(key) {
      store.delete(key)
    },
    async list({ prefix, limit }) {
      const keys = [...store.keys()]
        .filter((k) => k.startsWith(prefix))
        .sort()
        .slice(0, limit)
        .map((name) => ({ name, metadata: store.get(name).metadata }))
      return { keys }
    },
  }
}

function fakeMessage(to = 'rinda@inbox.example.com') {
  return {
    to,
    from: 'shop@example.com',
    raw: new Response('Subject: hi\r\n\r\nbody').body,
    setReject: vi.fn(),
    forward: vi.fn(),
  }
}

const baseEnv = () => ({ INGEST_URL: 'https://ingest.test/api/ingest', INGEST_SECRET: 's'.repeat(64) })

function mockFetch(...statuses) {
  const fn = vi.fn()
  for (const status of statuses) {
    if (status === 'network') fn.mockRejectedValueOnce(new TypeError('fetch failed'))
    else fn.mockResolvedValueOnce(new Response('{}', { status }))
  }
  vi.stubGlobal('fetch', fn)
  return fn
}

beforeEach(() => vi.spyOn(console, 'log').mockImplementation(() => {}))
afterEach(() => vi.unstubAllGlobals())

describe('deliver', () => {
  it.each([
    [201, Outcome.STORED],
    [200, Outcome.STORED],
    [404, Outcome.UNKNOWN_RECIPIENT],
    [401, Outcome.REJECTED],
    [422, Outcome.REJECTED],
    [502, Outcome.UNAVAILABLE],
    ['network', Outcome.UNAVAILABLE],
  ])('HTTP %s -> %s', async (status, outcome) => {
    mockFetch(status)
    expect((await deliver(baseEnv(), new ArrayBuffer(1), 'a@b', 'c@d')).outcome).toBe(outcome)
  })

  it('sends the secret and envelope headers', async () => {
    const fetchMock = mockFetch(201)
    await deliver(baseEnv(), new ArrayBuffer(1), 'to@inbox.example.com', 'from@x')
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('https://ingest.test/api/ingest')
    expect(init.headers['X-Ingest-Secret']).toBe('s'.repeat(64))
    expect(init.headers['X-Envelope-To']).toBe('to@inbox.example.com')
  })
})

describe('email handler', () => {
  it('stores and optionally forwards', async () => {
    mockFetch(201)
    const message = fakeMessage()
    await worker.email(message, { ...baseEnv(), FORWARD_TO: 'me@example.com' })
    expect(message.setReject).not.toHaveBeenCalled()
    expect(message.forward).toHaveBeenCalledWith('me@example.com')
  })

  it('rejects unknown recipients', async () => {
    mockFetch(404)
    const message = fakeMessage()
    await worker.email(message, baseEnv())
    expect(message.setReject).toHaveBeenCalledWith('Unknown recipient')
  })

  it('queues the message when the backend is down', async () => {
    mockFetch('network')
    const env = { ...baseEnv(), PENDING: fakeKV(), PENDING_TTL_SECONDS: '3600' }
    const message = fakeMessage()
    await worker.email(message, env)
    expect(message.setReject).not.toHaveBeenCalled()
    const [entry] = env.PENDING.store.values()
    expect(entry.metadata.to).toBe('rinda@inbox.example.com')
    expect(entry.ttl).toBe(3600)
  })

  it('throws (temporary failure) when the backend is down and no queue is bound', async () => {
    mockFetch(503)
    await expect(worker.email(fakeMessage(), baseEnv())).rejects.toThrow('Ingest unavailable')
  })

  it('throws when not configured', async () => {
    await expect(worker.email(fakeMessage(), {})).rejects.toThrow('must be configured')
  })
})

describe('retryPending', () => {
  async function queued(count) {
    const env = { ...baseEnv(), PENDING: fakeKV() }
    for (let i = 0; i < count; i++) {
      await enqueue(env, new TextEncoder().encode(`m${i}`).buffer, `u${i}@inbox.example.com`, 'x', 1000 + i)
    }
    return env
  }

  it('delivers oldest first and drops permanently rejected messages', async () => {
    const env = await queued(3)
    const fetchMock = mockFetch(201, 404, 201)
    expect(await retryPending(env)).toEqual({ delivered: 2, dropped: 1, remaining: 0 })
    expect(fetchMock.mock.calls.map(([, init]) => init.headers['X-Envelope-To'])).toEqual([
      'u0@inbox.example.com',
      'u1@inbox.example.com',
      'u2@inbox.example.com',
    ])
    expect(env.PENDING.store.size).toBe(0)
  })

  it('stops at the first failure and keeps the rest', async () => {
    const env = await queued(3)
    mockFetch(201, 502)
    expect(await retryPending(env)).toEqual({ delivered: 1, dropped: 0, remaining: 2 })
    expect([...env.PENDING.store.keys()].every((k) => k.startsWith(PENDING_PREFIX))).toBe(true)
    expect(env.PENDING.store.size).toBe(2)
  })

  it('does not let one message the backend always fails on block the ones behind it', async () => {
    const env = await queued(3)
    // u0 gets HTTP 500 (the backend is up and chokes on this message), u1 and u2 are fine
    const fetchMock = mockFetch(500, 201, 201)
    expect(await retryPending(env)).toEqual({ delivered: 2, dropped: 0, remaining: 1 })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    const [[name, entry]] = [...env.PENDING.store.entries()]
    expect(name).toContain(String(1000).padStart(15, '0'))
    expect(entry.metadata.attempts).toBe(1)
  })

  it.each([502, 503, 504, 530, 'network'])('still treats %s as an outage and stops', async (status) => {
    const env = await queued(3)
    const fetchMock = mockFetch(status)
    expect(await retryPending(env)).toEqual({ delivered: 0, dropped: 0, remaining: 3 })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(env.PENDING.store.size).toBe(3)
  })

  it('drops a message after PENDING_MAX_ATTEMPTS failed deliveries', async () => {
    const env = await queued(2)
    env.PENDING_MAX_ATTEMPTS = '2'
    mockFetch(500, 201) // run 1: u0 fails once, u1 stored
    expect(await retryPending(env)).toEqual({ delivered: 1, dropped: 0, remaining: 1 })
    mockFetch(500) // run 2: second failure reaches the limit
    expect(await retryPending(env)).toEqual({ delivered: 0, dropped: 1, remaining: 0 })
    expect(env.PENDING.store.size).toBe(0)
  })

  it('keeps the original expiry when it records an attempt', async () => {
    const env = { ...baseEnv(), PENDING: fakeKV(), PENDING_TTL_SECONDS: '3600' }
    const queuedAt = Date.now() - 600 * 1000
    await enqueue(env, new TextEncoder().encode('m').buffer, 'u@inbox.example.com', 'x', queuedAt)
    mockFetch(500)
    await retryPending(env)
    const [entry] = env.PENDING.store.values()
    expect(entry.ttl).toBeGreaterThan(2900)
    expect(entry.ttl).toBeLessThanOrEqual(3000)
  })

  it('respects RETRY_BATCH_SIZE', async () => {
    const env = await queued(5)
    env.RETRY_BATCH_SIZE = '2'
    mockFetch(201, 201)
    expect((await retryPending(env)).delivered).toBe(2)
    expect(env.PENDING.store.size).toBe(3)
  })
})
