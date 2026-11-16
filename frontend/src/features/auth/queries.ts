import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { authApi } from './api'
import { authKeys } from './keys'
import { clearUserData, resetSession } from './session'

/** The current user; `data` is null when logged out. Cached until login, logout or a 401. */
export function useMe() {
  return useQuery({
    queryKey: authKeys.me,
    queryFn: authApi.me,
    staleTime: Infinity,
  })
}

export function useIsAdmin() {
  return useMe().data?.role === 'admin'
}

export function useLogin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) => authApi.login(email, password),
    onSuccess: (user) => {
      // Nothing cached before the login may be shown to this user
      clearUserData()
      queryClient.setQueryData(authKeys.me, user)
    },
  })
}

export function useLogout() {
  return useMutation({
    mutationFn: authApi.logout,
    // Even when the request fails the browser forgets the session
    onSettled: resetSession,
  })
}
