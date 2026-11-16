import { expect, test, type APIRequestContext } from '@playwright/test'
import { envValue } from './env.ts'

// The ingest server is only reachable through the Cloudflare tunnel (or inside Docker)
const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:8081/api/ingest'
const INGEST_SECRET = envValue('INGEST_SECRET')
// An existing user of the stack under test (create one with `./api user add`). Normal users are
// enough: the test only generates, reads and deletes its own account.
const E2E_EMAIL = envValue('E2E_EMAIL')
const E2E_PASSWORD = envValue('E2E_PASSWORD')

const createdAccounts: string[] = []

async function ingest(request: APIRequestContext, to: string, subject: string, html: string) {
  const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const raw = [
    'From: "E2E Shop" <noreply@e2e-shop.example>',
    `To: ${to}`,
    `Subject: ${subject}`,
    `Message-ID: <${id}@e2e-shop.example>`,
    'MIME-Version: 1.0',
    'Content-Type: text/html; charset=utf-8',
    '',
    html,
    '',
  ].join('\r\n')
  const response = await request.post(INGEST_URL, {
    headers: { 'X-Ingest-Secret': INGEST_SECRET!, 'X-Envelope-To': to, 'Content-Type': 'message/rfc822' },
    data: raw,
  })
  expect(response.status(), await response.text()).toBe(201)
}

/** Logs in through the API; the session cookie stays in the request context's cookie jar. */
async function login(request: APIRequestContext) {
  const response = await request.post('/api/auth/login', {
    data: { email: E2E_EMAIL, password: E2E_PASSWORD },
  })
  expect(response.status(), 'login failed: check E2E_EMAIL / E2E_PASSWORD').toBe(200)
}

// Accounts are only visible to their owner, so clean up with a fresh login
test.afterAll(async ({ playwright, baseURL }) => {
  if (!createdAccounts.length) return
  const api = await playwright.request.newContext({ baseURL })
  await login(api)
  for (const id of createdAccounts) await api.delete(`/api/accounts/${id}`)
  await api.dispose()
})

test('log in, generate an account, receive an OTP email live, copy the code', async ({ page }) => {
  test.skip(!INGEST_SECRET, 'INGEST_SECRET is not available (env or ../.env)')
  test.skip(!E2E_EMAIL || !E2E_PASSWORD, 'E2E_EMAIL / E2E_PASSWORD are not available (env or ../.env)')
  const tag = `e2e-${Date.now()}`
  // page.request shares the page's cookies, so API calls below run as the logged-in user
  const request = page.request

  // Unauthenticated visits go to the login page and come back after signing in
  await page.goto('/accounts')
  await expect(page).toHaveURL(/\/login/)
  await page.getByLabel('Email').fill(E2E_EMAIL!)
  await page.getByLabel('Password').fill(E2E_PASSWORD!)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/accounts/)

  // Generate through the UI
  await page.getByRole('button', { name: 'Generate Account' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('combobox', { name: 'Provider' })).toContainText('Own domain')
  await dialog.getByLabel('Tag').fill(tag)
  await dialog.getByRole('button', { name: 'Generate', exact: true }).click()
  await expect(dialog).toBeHidden()

  const list = await request.get(`/api/accounts?search=${tag}`)
  const [account] = (await list.json()).data as { id: string; email: string }[]
  createdAccounts.push(account.id)
  expect(account.email).toMatch(/^[a-z]+[._]?[a-z]+\d{0,4}@/)

  // Open the inbox, then deliver mail: it must appear without reloading
  // Wait until the live stream is open: an event sent before that is not replayed, so on a slow
  // machine (CI) the toast would never show. The server subscribes before it answers.
  const streamOpen = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/events')
  await page.goto(`/inbox?account=${account.id}`)
  await streamOpen
  await ingest(
    request,
    account.email,
    'Kode verifikasi E2E',
    '<p>Kode verifikasi kamu: <b>735 209</b></p><p><a href="https://example.com/verify?token=e2eTokenAbcdefgh123">Verify email</a></p>',
  )
  await expect(page.getByText(`New email for ${account.email}`)).toBeVisible({ timeout: 15_000 })
  await page.getByText('Kode verifikasi E2E').first().click()

  // OTP and verification link cards
  await expect(page.locator('[data-otp-code]')).toHaveText('735209')
  await expect(page.getByRole('link', { name: 'Open', exact: true })).toHaveAttribute(
    'href',
    'https://example.com/verify?token=e2eTokenAbcdefgh123',
  )
  await page.getByRole('button', { name: 'Copy OTP to clipboard' }).click()
  await expect(page.getByText('OTP copied to clipboard')).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('735209')

  // Expanded view shows the same message in a wide modal
  await page.getByRole('button', { name: 'Expand message' }).click()
  const expanded = page.getByRole('dialog', { name: 'Kode verifikasi E2E' })
  await expect(expanded.locator('[data-otp-code]')).toHaveText('735209')
  await expect(expanded.getByTitle('Message content')).toBeVisible()
  expect((await expanded.boundingBox())!.width).toBeGreaterThan(900)
  await page.keyboard.press('Escape')
  await expect(expanded).toBeHidden()
})
