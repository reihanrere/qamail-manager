export type UserRole = 'admin' | 'user'

/** The logged-in user (`GET /api/auth/me`). */
export interface AuthUser {
  id: string
  email: string
  role: UserRole
}
