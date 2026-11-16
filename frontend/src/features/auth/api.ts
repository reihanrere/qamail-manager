import { isAxiosError } from 'axios'
import { api } from '@/lib/axios'
import type { ApiResponse } from '@/types/api'
import type { AuthUser } from '@/types/auth'

export const authApi = {
  /** The logged-in user, or null when there is no valid session. */
  me: async (): Promise<AuthUser | null> => {
    try {
      const response = await api.get<ApiResponse<AuthUser>>('/auth/me')
      return response.data.data
    } catch (error) {
      if (isAxiosError(error) && error.response?.status === 401) return null
      throw error
    }
  },

  login: async (email: string, password: string): Promise<AuthUser> => {
    const response = await api.post<ApiResponse<AuthUser>>('/auth/login', { email, password })
    return response.data.data
  },

  logout: async (): Promise<void> => {
    await api.post('/auth/logout')
  },
}
