import { useState, type FormEvent } from 'react'
import { Navigate, createFileRoute, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { LanguageToggle } from '@/components/layout/app-header'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { apiErrorMessage } from '@/lib/api-error'
import { safeRedirect } from '@/lib/safe-redirect'
import { useLogin, useMe } from '@/features/auth/queries'

export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
  component: LoginPage,
})

function LoginPage() {
  const { t } = useTranslation()
  const router = useRouter()
  const { redirect } = Route.useSearch()
  const me = useMe()
  const login = useLogin()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  if (me.data) return <Navigate to={safeRedirect(redirect)} replace />

  const submit = (event: FormEvent) => {
    event.preventDefault()
    login.mutate(
      { email: email.trim(), password },
      { onSuccess: () => router.history.replace(safeRedirect(redirect)) },
    )
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <div className="absolute top-4 right-4">
        <LanguageToggle />
      </div>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{t('auth.loginTitle')}</CardTitle>
          <CardDescription>{t('auth.loginDescription')}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="grid gap-4">
            <div className="grid gap-2">
              <Label htmlFor="login-email">{t('auth.email')}</Label>
              <Input
                id="login-email"
                type="email"
                autoComplete="username"
                autoFocus
                required
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                disabled={login.isPending}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="login-password">{t('auth.password')}</Label>
              <Input
                id="login-password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                disabled={login.isPending}
              />
            </div>
            {login.isError && (
              <p role="alert" className="text-sm text-destructive">
                {apiErrorMessage(login.error, t('auth.loginFailed'))}
              </p>
            )}
            <Button type="submit" disabled={login.isPending || !email || !password} className="gap-2">
              {login.isPending && <Loader2 className="size-4 animate-spin" />}
              {login.isPending ? t('auth.signingIn') : t('auth.signIn')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
