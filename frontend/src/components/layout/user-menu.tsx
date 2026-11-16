import { useTranslation } from 'react-i18next'
import { LogOut } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useLogout, useMe } from '@/features/auth/queries'

/** Who is logged in, their role, and the logout button (icon only in a collapsed sidebar). */
export function UserMenu({ collapsed = false }: { collapsed?: boolean }) {
  const { t } = useTranslation()
  const user = useMe().data
  const logout = useLogout()
  if (!user) return null

  const logoutButton = (
    <Button
      variant="ghost"
      size="icon"
      className="shrink-0"
      onClick={() => logout.mutate()}
      disabled={logout.isPending}
      aria-label={t('auth.logout')}
      title={t('auth.logout')}
    >
      <LogOut className="size-4" />
    </Button>
  )
  if (collapsed) return <div className="flex justify-center p-2">{logoutButton}</div>

  return (
    <div className="flex items-center gap-2 p-2 pl-3">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium" title={user.email}>
          {user.email}
        </p>
        <Badge variant={user.role === 'admin' ? 'default' : 'secondary'} className="mt-0.5">
          {t(`auth.role.${user.role}`)}
        </Badge>
      </div>
      {logoutButton}
    </div>
  )
}
