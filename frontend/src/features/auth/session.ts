import { queryClient } from '@/lib/query-client'
import { authKeys } from './keys'

/**
 * Forgets the session: drops every cached query (so the next user never sees this user's
 * accounts) and marks nobody as logged in, which sends the root guard to the login page.
 */
export function resetSession() {
  clearUserData()
  queryClient.setQueryData(authKeys.me, null)
}

/**
 * Removes every cached query except the session itself. queryClient.clear() would also drop
 * the session query and leave mounted components watching a query that no longer exists.
 */
export function clearUserData() {
  queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== authKeys.me[0] })
}

export function hasSession(): boolean {
  return !!queryClient.getQueryData(authKeys.me)
}
