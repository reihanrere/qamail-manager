import { useQuery } from '@tanstack/react-query'
import { userApi } from './api'

export const userKeys = {
  all: ['users'] as const,
}

/** Every user with their account count and quota. Admin only: other roles get 403. */
export function useUsers(enabled = true) {
  return useQuery({ queryKey: userKeys.all, queryFn: userApi.list, enabled })
}
