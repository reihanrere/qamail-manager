import axios, { isAxiosError } from 'axios'
import { hasSession, resetSession } from '@/features/auth/session'

/**
 * Backend API client. `VITE_API_URL` is absolute in local development
 * (http://localhost:8080/api) and relative (`/api`) behind the Docker nginx proxy.
 * The session lives in an HttpOnly cookie, so requests carry credentials.
 */
export const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
  withCredentials: true,
})

// A 401 from anywhere but the auth endpoints means the session ended (expired, revoked or the
// user was disabled): drop it so the root guard shows the login page.
api.interceptors.response.use(undefined, (error: unknown) => {
  if (
    isAxiosError(error) &&
    error.response?.status === 401 &&
    !error.config?.url?.startsWith('/auth/') &&
    hasSession()
  ) {
    resetSession()
  }
  return Promise.reject(error)
})
