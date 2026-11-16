import { describe, expect, it } from 'vitest'
import { safeRedirect } from './safe-redirect'

describe('safeRedirect', () => {
  it('keeps paths inside the app', () => {
    expect(safeRedirect('/inbox?account=1')).toBe('/inbox?account=1')
  })

  it('falls back to the dashboard for anything else', () => {
    for (const bad of [undefined, '', '//evil.example', 'https://evil.example', '/login', 'inbox']) {
      expect(safeRedirect(bad)).toBe('/dashboard')
    }
  })
})
