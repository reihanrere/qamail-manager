import { api } from '@/lib/axios'
import type { ApiResponse } from '@/types/api'
import type { CreateUserRequest, ManagedUser, UpdateUserRequest } from '@/types/user'

/** User-management endpoints of the backend API (admin only). */
export const userApi = {
  list: async (): Promise<ManagedUser[]> => {
    const response = await api.get<ApiResponse<ManagedUser[]>>('/users')
    return response.data.data ?? []
  },

  create: async (data: CreateUserRequest): Promise<ManagedUser> => {
    const response = await api.post<ApiResponse<ManagedUser>>('/users', data)
    return response.data.data
  },

  update: async (id: string, data: UpdateUserRequest): Promise<void> => {
    await api.patch(`/users/${id}`, data)
  },

  /** `force` also deletes the user's mail accounts; without it the server answers 409 if there are any. */
  delete: async (id: string, force: boolean): Promise<void> => {
    await api.delete(`/users/${id}`, { params: force ? { force: true } : undefined })
  },
}
