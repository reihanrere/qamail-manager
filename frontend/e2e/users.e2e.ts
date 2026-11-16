import { expect, test, type APIRequestContext } from '@playwright/test'
import { envValue } from './env.ts'

// An existing admin of the stack under test (`./api user add ... --admin`)
const ADMIN_EMAIL = envValue('E2E_ADMIN_EMAIL')
const ADMIN_PASSWORD = envValue('E2E_ADMIN_PASSWORD')

const NEW_USER_PASSWORD = 'e2e-new-user-password'

async function login(request: APIRequestContext, email: string, password: string) {
  return request.post('/api/auth/login', { data: { email, password } })
}

test('an admin adds a user who sees only their own accounts; disabling signs them out', async ({
  page,
  playwright,
  baseURL,
}) => {
  test.skip(
    !ADMIN_EMAIL || !ADMIN_PASSWORD,
    'E2E_ADMIN_EMAIL / E2E_ADMIN_PASSWORD are not available (env or ../.env)',
  )
  const stamp = Date.now()
  const newEmail = `e2e-user-${stamp}@example.com`
  const adminTag = `e2e-admin-${stamp}`
  const userTag = `e2e-user-${stamp}`
  // page.request shares the page's cookies, so API calls below run as the logged-in admin
  const adminApi = page.request
  const userApi = await playwright.request.newContext({ baseURL })
  let adminAccountId: string | undefined
  let newUserId: string | undefined

  try {
    // Admin signs in and opens the Users page from the sidebar
    await page.goto('/users')
    await expect(page).toHaveURL(/\/login/)
    await page.getByLabel('Email').fill(ADMIN_EMAIL!)
    await page.getByLabel('Password').fill(ADMIN_PASSWORD!)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL(/\/users/)
    await expect(page.getByRole('link', { name: 'Users' })).toBeVisible()

    // An account of the admin, which the new user must never see
    const generated = await adminApi.post('/api/accounts/generate', { data: { tag: adminTag } })
    expect(generated.status(), await generated.text()).toBe(201)
    adminAccountId = (await generated.json()).data.id

    // Add the user through the UI
    await page.getByRole('button', { name: 'Add user' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Email').fill(newEmail)
    await dialog.getByLabel('Password').fill(NEW_USER_PASSWORD)
    await dialog.getByRole('button', { name: 'Add user' }).click()
    await expect(dialog).toBeHidden()
    const row = page.getByRole('row', { name: new RegExp(newEmail) })
    await expect(row).toContainText('Active')
    newUserId = (
      (await (await adminApi.get('/api/users')).json()).data as { id: string; email: string }[]
    ).find((u) => u.email === newEmail)?.id

    // The new user logs in, owns one account, and sees neither the admin's nor the Users API
    const loggedIn = await login(userApi, newEmail, NEW_USER_PASSWORD)
    expect(loggedIn.status(), await loggedIn.text()).toBe(200)
    const own = await userApi.post('/api/accounts/generate', { data: { tag: userTag } })
    expect(own.status(), await own.text()).toBe(201)
    const visible = ((await (await userApi.get('/api/accounts')).json()).data ?? []) as {
      id: string
      tag: string
    }[]
    expect(visible.map((a) => a.tag)).toEqual([userTag])
    expect((await userApi.get(`/api/accounts/${adminAccountId}`)).status()).toBe(404)
    expect((await userApi.get('/api/users')).status()).toBe(403)

    // Disabling the user in the UI signs them out and blocks the next login
    await row.getByRole('button', { name: 'Disable user' }).click()
    await expect(page.getByText(`${newEmail} disabled and signed out`)).toBeVisible()
    await expect(row).toContainText('Disabled')
    expect((await userApi.get('/api/auth/me')).status()).toBe(401)
    expect((await login(userApi, newEmail, NEW_USER_PASSWORD)).status()).toBe(401)

    // Deleting a user who owns an account asks for confirmation and takes the account along
    await row.getByRole('button', { name: 'Delete user' }).click()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText('This also deletes the 1 mail account')
    await confirm.getByRole('button', { name: 'Delete' }).click()
    await expect(row).toBeHidden()
    newUserId = undefined
  } finally {
    // Leave nothing behind, whatever failed above
    if (newUserId) await adminApi.delete(`/api/users/${newUserId}?force=true`)
    if (adminAccountId) await adminApi.delete(`/api/accounts/${adminAccountId}`)
    await userApi.dispose()
  }
})
