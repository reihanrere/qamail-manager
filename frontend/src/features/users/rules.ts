import type { AuthUser } from '@/types/auth'
import type { ManagedUser } from '@/types/user'

/** Why disabling or deleting a user is not offered: yourself, or the last enabled admin. */
export type UserActionBlock = 'self' | 'lastAdmin'

/**
 * The backend refuses these too (409); the UI disables the buttons and says why instead of
 * letting the admin find out from an error.
 */
export function userActionBlock(
  target: ManagedUser,
  currentUserId: string | undefined,
  users: ManagedUser[],
): UserActionBlock | null {
  if (target.id === currentUserId) return 'self'
  if (target.role === 'admin' && !target.disabled) {
    const otherEnabledAdmins = users.filter((u) => u.id !== target.id && u.role === 'admin' && !u.disabled)
    if (otherEnabledAdmins.length === 0) return 'lastAdmin'
  }
  return null
}

/**
 * What the Users page does for the current user: show itself (admin), send a plain user to the
 * dashboard, or do nothing when there is no session (the root guard sends that browser to
 * /login; redirecting as well would make the two guards bounce between /login and /dashboard).
 */
export function usersPageAccess(user: AuthUser | null | undefined): 'allow' | 'redirect' | 'none' {
  if (!user) return 'none'
  return user.role === 'admin' ? 'allow' : 'redirect'
}
