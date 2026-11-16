import { Navigate, Outlet, createRootRoute, useRouterState } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AppLayout } from '@/components/layout/app-layout'
import { Button } from '@/components/ui/button'
import { useLiveEvents } from '@/features/live/use-live-events'
import { useMe } from '@/features/auth/queries'

export const Route = createRootRoute({
  component: RootComponent,
})

/** Every route except /login needs a session: others see a spinner, then the login page. */
function RootComponent() {
  const { t } = useTranslation()
  const location = useRouterState({ select: (state) => state.location })
  const me = useMe()

  if (location.pathname === '/login') return <Outlet />
  if (me.isPending) {
    return (
      <div className="flex h-screen items-center justify-center text-sm text-muted-foreground">
        {t('auth.checkingSession')}
      </div>
    )
  }
  if (me.isError) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-3">
        <p className="text-sm text-destructive">{t('auth.sessionCheckFailed')}</p>
        <Button variant="outline" onClick={() => me.refetch()}>
          {t('common.retry')}
        </Button>
      </div>
    )
  }
  if (!me.data) {
    return <Navigate to="/login" search={{ redirect: location.pathname + location.searchStr }} replace />
  }
  return <AuthenticatedShell />
}

function AuthenticatedShell() {
  useLiveEvents()
  return (
    <AppLayout>
      <Outlet />
    </AppLayout>
  )
}
