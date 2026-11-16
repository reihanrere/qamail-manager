import { describe, expect, it } from 'vitest'
import { userActionBlock, usersPageAccess } from './rules'
import type { ManagedUser } from '@/types/user'

function user(id: string, over: Partial<ManagedUser> = {}): ManagedUser {
  return {
    id,
    email: `${id}@example.com`,
    role: 'user',
    disabled: false,
    accounts: 0,
    quota: { used: 0, limit: 10, remaining: 10 },
    createdAt: '2026-11-13T00:00:00Z',
    ...over,
  }
}

describe('userActionBlock', () => {
  const root = user('root', { role: 'admin' })
  const boss = user('boss', { role: 'admin' })
  const alice = user('alice')

  it('allows everything on another plain user', () => {
    expect(userActionBlock(alice, 'root', [root, alice])).toBeNull()
  })

  it('blocks disabling and deleting yourself, with the reason "self"', () => {
    expect(userActionBlock(root, 'root', [root, boss, alice])).toBe('self')
  })

  it('blocks the last enabled admin, with the reason "lastAdmin"', () => {
    // The acting admin is not in the list as an enabled admin (a stale session): still protected
    expect(userActionBlock(root, 'someone-else', [root, alice])).toBe('lastAdmin')
    expect(userActionBlock(root, 'someone-else', [root, { ...boss, disabled: true }])).toBe('lastAdmin')
  })

  it('allows an admin when another enabled admin remains', () => {
    expect(userActionBlock(boss, 'root', [root, boss])).toBeNull()
  })

  it('never blocks a disabled admin (enabling and deleting it are safe)', () => {
    expect(
      userActionBlock({ ...boss, disabled: true }, 'root', [root, { ...boss, disabled: true }]),
    ).toBeNull()
  })
})

describe('usersPageAccess', () => {
  it('lets an admin in', () => {
    expect(usersPageAccess({ id: 'a', email: 'a@example.com', role: 'admin' })).toBe('allow')
  })

  it('sends a plain user to the dashboard', () => {
    expect(usersPageAccess({ id: 'a', email: 'a@example.com', role: 'user' })).toBe('redirect')
  })

  // Without a session the root guard is already sending the browser to /login. Redirecting
  // here too bounces /login and /dashboard forever, because the router still renders this
  // route's page for a moment while /login loads.
  it('does nothing without a session, so it cannot fight the login redirect', () => {
    expect(usersPageAccess(null)).toBe('none')
    expect(usersPageAccess(undefined)).toBe('none')
  })
})
