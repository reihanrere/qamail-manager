import type { AccountQuota } from '@/types/account'
import type { UserRole } from '@/types/auth'

/** One user as the admin sees them (`GET /api/users`). Never carries a password. */
export interface ManagedUser {
  id: string
  email: string
  role: UserRole
  disabled: boolean
  /** Every mail account the user owns, blocked ones included */
  accounts: number
  quota: AccountQuota
  createdAt: string
}

export interface CreateUserRequest {
  email: string
  password: string
  role: UserRole
}

export interface UpdateUserRequest {
  disabled?: boolean
  password?: string
}
