/** Only paths inside this app: "/x" is fine, "//evil.example" and "https://..." are not. */
export function safeRedirect(target: string | undefined): string {
  return target && target.startsWith('/') && !target.startsWith('//') && !target.startsWith('/login')
    ? target
    : '/dashboard'
}
