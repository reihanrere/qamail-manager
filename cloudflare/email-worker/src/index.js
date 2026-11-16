/**
 * Cloudflare Email Worker for QAMail Manager.
 *
 * Email Routing sends every message for the catch-all domain here. The raw message is
 * POSTed to the backend's ingest server (through the Cloudflare tunnel), which stores it
 * for the matching "local" account.
 *
 * When the backend cannot be reached (server down, Docker stopped) the message is kept
 * in the PENDING KV namespace and a cron trigger re-delivers it once the backend is back,
 * so mail is never lost while offline.
 *
 * Configuration (see wrangler.toml and README.md):
 *   INGEST_URL           var     e.g. https://ingest.example.com/api/ingest
 *   INGEST_SECRET        secret  same value as the backend's INGEST_SECRET
 *   FORWARD_TO           var     optional verified address that also receives a copy
 *   PENDING              KV      queue for messages the backend could not take yet
 *   PENDING_TTL_SECONDS  var     how long a queued message is kept (default 7 days)
 *   RETRY_BATCH_SIZE     var     queued messages re-delivered per cron run (default 50)
 *   PENDING_MAX_ATTEMPTS var     failed deliveries after which a message the backend keeps
 *                                answering with an error is dropped (default 10)
 */

export const PENDING_PREFIX = 'pending:'
const DEFAULT_TTL_SECONDS = 7 * 24 * 60 * 60
const DEFAULT_BATCH_SIZE = 50
const DEFAULT_MAX_ATTEMPTS = 10
// KV refuses an expirationTtl below 60 seconds
const MIN_KV_TTL_SECONDS = 60

/** Delivery outcomes of one POST to the ingest endpoint. */
export const Outcome = {
  STORED: 'stored', // 2xx
  UNKNOWN_RECIPIENT: 'unknown-recipient', // 404: address was never generated
  REJECTED: 'rejected', // other 4xx: will never succeed, do not retry
  UNAVAILABLE: 'unavailable', // network error or 5xx: retry later
}

/** POSTs a raw message to the backend and classifies the result. */
export async function deliver(env, raw, to, from) {
  let response
  try {
    response = await fetch(env.INGEST_URL, {
      method: 'POST',
      headers: {
        'Content-Type': 'message/rfc822',
        'X-Ingest-Secret': env.INGEST_SECRET,
        'X-Envelope-To': to,
        'X-Envelope-From': from ?? '',
      },
      body: raw,
    })
  } catch {
    return { outcome: Outcome.UNAVAILABLE, status: 0 }
  }
  if (response.ok) return { outcome: Outcome.STORED, status: response.status }
  if (response.status === 404) return { outcome: Outcome.UNKNOWN_RECIPIENT, status: 404 }
  if (response.status >= 400 && response.status < 500) return { outcome: Outcome.REJECTED, status: response.status }
  return { outcome: Outcome.UNAVAILABLE, status: response.status }
}

/**
 * True when the failure says the backend (or the tunnel in front of it) cannot be reached,
 * so every queued message would fail the same way. Any other 5xx means the backend is up and
 * failed on this one message.
 */
function isOutage(status) {
  return status === 0 || status === 502 || status === 503 || status === 504 || (status >= 520 && status <= 530)
}

function positiveInt(value, fallback) {
  const n = Number.parseInt(value ?? '', 10)
  return Number.isFinite(n) && n > 0 ? n : fallback
}

/** Keeps a message in KV until the backend is reachable again. */
export async function enqueue(env, raw, to, from, now = Date.now()) {
  // Time-ordered keys so the oldest message is retried first
  const key = `${PENDING_PREFIX}${String(now).padStart(15, '0')}:${crypto.randomUUID()}`
  await env.PENDING.put(key, raw, {
    expirationTtl: positiveInt(env.PENDING_TTL_SECONDS, DEFAULT_TTL_SECONDS),
    metadata: { to, from: from ?? '', queuedAt: new Date(now).toISOString() },
  })
  return key
}

/**
 * Re-delivers queued messages, oldest first. Stops when the backend cannot be reached, since
 * the rest would fail the same way. When the backend answers with an error for one message,
 * that message is retried on later runs (up to PENDING_MAX_ATTEMPTS) but does not hold back
 * the ones behind it: a sender who crafts a message that always fails must not block everyone.
 */
export async function retryPending(env, now = Date.now()) {
  const summary = { delivered: 0, dropped: 0, remaining: 0 }
  const limit = positiveInt(env.RETRY_BATCH_SIZE, DEFAULT_BATCH_SIZE)
  const maxAttempts = positiveInt(env.PENDING_MAX_ATTEMPTS, DEFAULT_MAX_ATTEMPTS)
  const ttl = positiveInt(env.PENDING_TTL_SECONDS, DEFAULT_TTL_SECONDS)
  const { keys } = await env.PENDING.list({ prefix: PENDING_PREFIX, limit })
  let retained = 0

  for (const { name, metadata } of keys) {
    const raw = await env.PENDING.get(name, 'arrayBuffer')
    if (raw === null) continue // expired between list and get
    const { outcome, status } = await deliver(env, raw, metadata?.to ?? '', metadata?.from ?? '')
    if (outcome === Outcome.UNAVAILABLE && isOutage(status)) {
      summary.remaining = keys.length - summary.delivered - summary.dropped
      return summary
    }
    if (outcome === Outcome.UNAVAILABLE) {
      const attempts = (metadata?.attempts ?? 0) + 1
      if (attempts >= maxAttempts) {
        console.log(`dropping queued message for ${metadata?.to}: HTTP ${status} on ${attempts} attempts`)
        await env.PENDING.delete(name)
        summary.dropped++
        continue
      }
      // Keep the message, with the time it has left of its original lifetime
      const queuedAt = Date.parse(metadata?.queuedAt ?? '') || now
      const left = Math.max(MIN_KV_TTL_SECONDS, ttl - Math.floor((now - queuedAt) / 1000))
      await env.PENDING.put(name, raw, { expirationTtl: left, metadata: { ...metadata, attempts } })
      retained++
      continue
    }
    // Stored, or permanently rejected (e.g. the account was deleted meanwhile)
    await env.PENDING.delete(name)
    if (outcome === Outcome.STORED) summary.delivered++
    else summary.dropped++
  }
  summary.remaining = retained
  return summary
}

export default {
  async email(message, env) {
    if (!env.INGEST_URL || !env.INGEST_SECRET) {
      // Throwing makes delivery fail temporarily, so the sender retries after the fix
      throw new Error('INGEST_URL and INGEST_SECRET must be configured')
    }

    const raw = await new Response(message.raw).arrayBuffer()
    const { outcome, status } = await deliver(env, raw, message.to, message.from)

    switch (outcome) {
      case Outcome.UNKNOWN_RECIPIENT:
        message.setReject('Unknown recipient')
        return
      case Outcome.REJECTED:
        message.setReject(`Rejected by ingest (${status})`)
        return
      case Outcome.UNAVAILABLE:
        if (!env.PENDING) {
          // No queue configured: fail so the sending server retries later
          throw new Error(`Ingest unavailable (HTTP ${status})`)
        }
        // The recipient cannot be checked while the backend is down; unknown addresses
        // are dropped when the queue is retried instead of being bounced now
        await enqueue(env, raw, message.to, message.from)
        console.log(`queued message for ${message.to}: ingest unavailable (HTTP ${status})`)
        break
    }

    if (env.FORWARD_TO) {
      await message.forward(env.FORWARD_TO)
    }
  },

  async scheduled(_event, env) {
    if (!env.PENDING || !env.INGEST_URL || !env.INGEST_SECRET) return
    const summary = await retryPending(env)
    if (summary.delivered || summary.dropped || summary.remaining) {
      console.log(`pending queue: ${JSON.stringify(summary)}`)
    }
  },
}
