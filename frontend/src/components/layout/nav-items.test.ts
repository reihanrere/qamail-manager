import { describe, expect, it } from 'vitest'
import { navItemsFor } from './nav-items'

describe('navItemsFor', () => {
  it('hides Users from a plain user', () => {
    expect(navItemsFor(false).map((item) => item.to)).toEqual([
      '/dashboard',
      '/accounts',
      '/inbox',
      '/settings',
    ])
  })

  it('shows Users to an admin', () => {
    expect(navItemsFor(true).map((item) => item.to)).toContain('/users')
  })
})
